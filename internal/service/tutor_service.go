package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// Book statuses used by the ingestion pipeline.
const (
	BookStatusPending    = "Pending"
	BookStatusProcessing = "Processing"
	BookStatusIndexed    = "Indexed"
	BookStatusFailed     = "Failed"
)

// TutorService owns the RAG pipeline: indexing a book's text into vector chunks
// (called from the ingestion worker) and answering a student's question from the
// textbooks that match their class + medium.
type TutorService struct {
	books    *repository.BookRepository
	chunks   *repository.BookChunkRepository
	embedder *ai.Embedder
	chat     *ai.Chat
	topK     int
}

// NewTutorService builds a TutorService.
func NewTutorService(
	books *repository.BookRepository,
	chunks *repository.BookChunkRepository,
	embedder *ai.Embedder,
	chat *ai.Chat,
	topK int,
) *TutorService {
	return &TutorService{books: books, chunks: chunks, embedder: embedder, chat: chat, topK: topK}
}

// Ingest indexes a book's text: it chunks the content, embeds each passage, and
// replaces the book's chunks. The book status is moved Processing → Indexed (or
// Failed on error). Runs in the background worker, not the request path.
func (s *TutorService) Ingest(ctx context.Context, bookID uint, content string) error {
	book, err := s.books.FindByID(bookID)
	if err != nil {
		return fmt.Errorf("load book %d: %w", bookID, err)
	}
	s.setStatus(book, BookStatusProcessing)

	pieces := ai.Chunk(content)
	if len(pieces) == 0 {
		s.setStatus(book, BookStatusFailed)
		return fmt.Errorf("book %d: no text to index", bookID)
	}

	vecs, err := s.embedder.EmbedDocuments(ctx, pieces)
	if err != nil {
		s.setStatus(book, BookStatusFailed)
		return fmt.Errorf("embed book %d: %w", bookID, err)
	}
	if len(vecs) != len(pieces) {
		s.setStatus(book, BookStatusFailed)
		return fmt.Errorf("book %d: embedding count mismatch (%d/%d)", bookID, len(vecs), len(pieces))
	}

	// Replace any prior index for this book so re-ingest is idempotent.
	if err := s.chunks.DeleteByBook(bookID); err != nil {
		s.setStatus(book, BookStatusFailed)
		return fmt.Errorf("clear chunks for book %d: %w", bookID, err)
	}
	rows := make([]model.BookChunk, len(pieces))
	for i, p := range pieces {
		rows[i] = model.BookChunk{
			BookID:     bookID,
			ClassName:  book.ClassName,
			Subject:    book.Subject,
			Medium:     book.Medium,
			BookTitle:  book.Title,
			ChunkIndex: i,
			Content:    p,
			Embedding:  vecs[i],
		}
	}
	if err := s.chunks.Insert(rows); err != nil {
		s.setStatus(book, BookStatusFailed)
		return fmt.Errorf("store chunks for book %d: %w", bookID, err)
	}
	// Refresh planner stats after the bulk load so the vector/btree indexes are
	// costed correctly (best-effort).
	s.chunks.Analyze()
	s.setStatus(book, BookStatusIndexed)
	return nil
}

// AskResult is a grounded answer plus the passages it drew on.
type AskResult struct {
	Answer   string                      `json:"answer"`
	Sources  []repository.RetrievedChunk `json:"sources"`
	Grounded bool                        `json:"grounded"`
}

// StudentContext carries the retrieval filter for a question.
type StudentContext struct {
	Class            string
	Medium           string
	Board            string
	Group            string
	TeachingLanguage string
}

// Ask answers a student's question from the textbooks that match their class +
// medium. It embeds the question, retrieves the closest passages, and asks
// Claude to answer strictly from them. If nothing is indexed for the student's
// class yet, it returns a clear "not available" answer rather than hallucinating.
func (s *TutorService) Ask(ctx context.Context, question string, sc StudentContext) (*AskResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("empty question")
	}
	qv, err := s.embedder.EmbedQuery(ctx, question)
	if err != nil {
		return nil, fmt.Errorf("embed question: %w", err)
	}
	sources, err := s.chunks.Search(qv, sc.Class, sc.Medium, "", s.topK)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	if len(sources) == 0 {
		return &AskResult{
			Answer: "I don't have the textbook for your class loaded yet, so I can't give a " +
				"textbook-based answer. Please check back soon — your teacher is adding the books.",
			Grounded: false,
		}, nil
	}

	answer, err := s.chat.Complete(ctx, tutorSystemPrompt(sc), buildUserPrompt(question, sources))
	if err != nil {
		return nil, fmt.Errorf("generate answer: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(answer), Sources: sources, Grounded: true}, nil
}

// AskStream is the streaming variant of Ask: it retrieves the textbook passages
// and then streams Claude's answer, invoking onDelta for each chunk as it is
// generated. The returned AskResult carries the full text + sources once done.
// When nothing is indexed for the class it streams the same "not loaded" message
// (ungrounded, so the caller shouldn't charge a credit).
func (s *TutorService) AskStream(ctx context.Context, question string, sc StudentContext, onDelta func(string)) (*AskResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("empty question")
	}
	qv, err := s.embedder.EmbedQuery(ctx, question)
	if err != nil {
		return nil, fmt.Errorf("embed question: %w", err)
	}
	sources, err := s.chunks.Search(qv, sc.Class, sc.Medium, "", s.topK)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	if len(sources) == 0 {
		msg := "I don't have the textbook for your class loaded yet, so I can't give a " +
			"textbook-based answer. Please check back soon — your teacher is adding the books."
		if onDelta != nil {
			onDelta(msg)
		}
		return &AskResult{Answer: msg, Grounded: false}, nil
	}
	full, err := s.chat.CompleteStream(ctx, tutorSystemPrompt(sc), buildUserPrompt(question, sources), onDelta)
	if err != nil {
		return nil, fmt.Errorf("generate answer: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(full), Sources: sources, Grounded: true}, nil
}

// Teach produces a short lesson for a homework task topic, tailored to the
// student's class + language and grounded in their textbook passages when any
// match. Unlike Ask (strict grounding), teaching may elaborate pedagogically
// (a simple explanation, a worked example, a quick check) so the student can
// actually learn the task — but it still leans on the textbook when available.
func (s *TutorService) Teach(ctx context.Context, topic string, sc StudentContext) (*AskResult, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, fmt.Errorf("empty topic")
	}
	qv, err := s.embedder.EmbedQuery(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("embed topic: %w", err)
	}
	sources, err := s.chunks.Search(qv, sc.Class, sc.Medium, "", s.topK)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	lesson, err := s.chat.Complete(ctx, teachSystemPrompt(sc), buildTeachPrompt(topic, sources))
	if err != nil {
		return nil, fmt.Errorf("generate lesson: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(lesson), Sources: sources, Grounded: len(sources) > 0}, nil
}

// AnswerDoubt answers a student's follow-up question about a specific homework
// task, grounded in their textbooks when a passage matches, in their language.
// More conversational than Ask (it always tries to help with the doubt), but
// still leans on the textbook and won't invent facts that contradict it.
func (s *TutorService) AnswerDoubt(ctx context.Context, topic, question string, sc StudentContext) (*AskResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("empty question")
	}
	qv, err := s.embedder.EmbedQuery(ctx, topic+" "+question)
	if err != nil {
		return nil, fmt.Errorf("embed doubt: %w", err)
	}
	sources, err := s.chunks.Search(qv, sc.Class, sc.Medium, "", s.topK)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	answer, err := s.chat.Complete(ctx, doubtSystemPrompt(sc), buildDoubtPrompt(topic, question, sources))
	if err != nil {
		return nil, fmt.Errorf("answer doubt: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(answer), Sources: sources, Grounded: len(sources) > 0}, nil
}

// Probe verifies the configured AI keys with a tiny round-trip: one embedding
// (Voyage) and one short completion (Claude). Used by the admin "Test AI" button
// so keys can be validated before books are ingested. Returns a clear error if
// either provider rejects the request.
func (s *TutorService) Probe(ctx context.Context) error {
	if _, err := s.embedder.EmbedQuery(ctx, "test"); err != nil {
		return fmt.Errorf("embeddings (Voyage): %w", err)
	}
	if _, err := s.chat.Complete(ctx, "You are a test.", "Reply with the single word OK."); err != nil {
		return fmt.Errorf("answers (Claude): %w", err)
	}
	return nil
}

func (s *TutorService) setStatus(b *model.Book, status string) {
	b.Status = status
	_ = s.books.Update(b)
}

// tutorSystemPrompt frames Claude as a grounded tutor for this student. The
// teaching language and board tailor tone; the grounding rules keep answers
// tied to the retrieved passages.
func tutorSystemPrompt(sc StudentContext) string {
	lang := sc.TeachingLanguage
	if lang == "" {
		lang = "the student's medium of instruction"
	}
	group := ""
	if sc.Group != "" {
		group = fmt.Sprintf(" (%s group)", sc.Group)
	}
	return fmt.Sprintf(`You are Vaha, a friendly, patient tutor for an Indian school student in %s%s, `+
		`%s board, studying in %s medium.

Answer using ONLY the textbook passages provided in the user's message. Rules:
- If the passages contain the answer, explain it simply and step by step, in a warm, encouraging tone a school student can follow. Prefer %s.
- If the passages do NOT contain the answer, say clearly that it isn't in the current textbook material, and do not invent facts from outside the passages.
- Keep it concise. Use short paragraphs or bullet points. Avoid jargon; when a technical term is needed, explain it.
- Never mention "passages", "context", "chunks", or that you were given excerpts — just teach.`,
		sc.Class, group, boardOrDefault(sc.Board), mediumOrDefault(sc.Medium), lang)
}

func buildUserPrompt(question string, sources []repository.RetrievedChunk) string {
	var b strings.Builder
	b.WriteString("Textbook passages:\n\n")
	for i, src := range sources {
		fmt.Fprintf(&b, "[%d] (%s — %s)\n%s\n\n", i+1, src.Subject, src.BookTitle, src.Content)
	}
	b.WriteString("Student's question:\n")
	b.WriteString(question)
	return b.String()
}

func teachSystemPrompt(sc StudentContext) string {
	lang := sc.TeachingLanguage
	if lang == "" {
		lang = "the student's medium of instruction"
	}
	group := ""
	if sc.Group != "" {
		group = fmt.Sprintf(" (%s group)", sc.Group)
	}
	return fmt.Sprintf(`You are Vaha, a friendly, patient tutor for an Indian school student in %s%s, `+
		`%s board, studying in %s medium. You are TEACHING one homework task.

Teach the task so the student truly understands it. Rules:
- Reply in %s (simple language the student can follow).
- Structure it: a one-line "what this is about", then a simple step-by-step explanation, then ONE short worked example, then a one-line "quick check" question at the end.
- Lean on the textbook passages provided when they're relevant; you may add gentle, standard, age-appropriate explanation to make it clear, but do NOT introduce facts that contradict the textbook.
- Warm and encouraging. Short paragraphs or bullets. Explain any technical term.
- Never mention "passages", "context", "chunks" or that you were given excerpts — just teach.`,
		sc.Class, group, boardOrDefault(sc.Board), mediumOrDefault(sc.Medium), lang)
}

func buildTeachPrompt(topic string, sources []repository.RetrievedChunk) string {
	var b strings.Builder
	if len(sources) > 0 {
		b.WriteString("Textbook passages (use where relevant):\n\n")
		for i, src := range sources {
			fmt.Fprintf(&b, "[%d] (%s — %s)\n%s\n\n", i+1, src.Subject, src.BookTitle, src.Content)
		}
	}
	b.WriteString("Homework task to teach:\n")
	b.WriteString(topic)
	return b.String()
}

func doubtSystemPrompt(sc StudentContext) string {
	lang := sc.TeachingLanguage
	if lang == "" {
		lang = "the student's medium of instruction"
	}
	return fmt.Sprintf(`You are Vaha, a friendly, patient tutor for an Indian school student in %s, `+
		`%s board, %s medium. The student is working on a homework task and has a DOUBT.

Answer the doubt clearly and simply. Rules:
- Reply in %s, in a warm, encouraging tone the student can follow.
- Use the textbook passages when they help; you may add gentle standard explanation, but never contradict the textbook or invent facts.
- Keep it short and focused on the doubt. Give a small example if it helps.
- Never mention "passages", "context" or that you were given excerpts — just answer.`,
		sc.Class, boardOrDefault(sc.Board), mediumOrDefault(sc.Medium), lang)
}

func buildDoubtPrompt(topic, question string, sources []repository.RetrievedChunk) string {
	var b strings.Builder
	if len(sources) > 0 {
		b.WriteString("Textbook passages (use where relevant):\n\n")
		for i, src := range sources {
			fmt.Fprintf(&b, "[%d] (%s — %s)\n%s\n\n", i+1, src.Subject, src.BookTitle, src.Content)
		}
	}
	b.WriteString("Homework task: ")
	b.WriteString(topic)
	b.WriteString("\n\nStudent's doubt: ")
	b.WriteString(question)
	return b.String()
}

func boardOrDefault(b string) string {
	if b == "" {
		return "State"
	}
	return b
}

func mediumOrDefault(m string) string {
	if m == "" {
		return "English"
	}
	return m
}
