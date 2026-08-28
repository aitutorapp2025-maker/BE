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
		// No textbook indexed for this class — answer from general knowledge
		// (class-appropriate, in the student's language) instead of refusing.
		answer, aerr := s.chat.Complete(ctx, generalTutorSystemPrompt(sc), question)
		if aerr != nil {
			return nil, fmt.Errorf("generate answer: %w", aerr)
		}
		return &AskResult{Answer: strings.TrimSpace(answer), Grounded: false}, nil
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
		// No textbook indexed for this class — stream a general-knowledge
		// answer (class-appropriate, student's language) instead of refusing.
		full, aerr := s.chat.CompleteStream(ctx, generalTutorSystemPrompt(sc), question, onDelta)
		if aerr != nil {
			return nil, fmt.Errorf("generate answer: %w", aerr)
		}
		return &AskResult{Answer: strings.TrimSpace(full), Grounded: false}, nil
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

// TeachStream is the streaming variant of Teach: it streams the lesson text via
// onDelta as it's generated and returns the full text once done.
func (s *TutorService) TeachStream(ctx context.Context, topic string, sc StudentContext, onDelta func(string)) (*AskResult, error) {
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
	full, err := s.chat.CompleteStream(ctx, teachSystemPrompt(sc), buildTeachPrompt(topic, sources), onDelta)
	if err != nil {
		return nil, fmt.Errorf("generate lesson: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(full), Sources: sources, Grounded: len(sources) > 0}, nil
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

// AnswerDoubtImage answers a doubt the student sent as a PHOTO (a question
// from their book/notebook, possibly handwritten): the vision model reads the
// image and explains the solution step by step, bilingually (content in the
// medium, explanation in the teaching language). An optional typed/spoken
// question accompanies the image.
func (s *TutorService) AnswerDoubtImage(ctx context.Context, topic, question, imageB64, mediaType string, sc StudentContext) (*AskResult, error) {
	if strings.TrimSpace(imageB64) == "" {
		return nil, fmt.Errorf("empty image")
	}
	q := strings.TrimSpace(question)
	if q == "" {
		q = "Please read the question in this photo and explain the solution step by step."
	}
	prompt := "Homework task the student is learning: " + topic +
		"\n\nThe student sent a PHOTO of a question (from their book or notebook — it may be handwritten). " +
		"Read it carefully, then: " + q
	answer, err := s.chat.CompleteVision(ctx, doubtSystemPrompt(sc), prompt, imageB64, mediaType)
	if err != nil {
		return nil, fmt.Errorf("answer photo doubt: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(answer)}, nil
}

// AnswerDoubtStream is the streaming variant of AnswerDoubt: it streams the
// answer via onDelta as it's generated and returns the full text once done.
func (s *TutorService) AnswerDoubtStream(ctx context.Context, topic, question string, sc StudentContext, onDelta func(string)) (*AskResult, error) {
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
	full, err := s.chat.CompleteStream(ctx, doubtSystemPrompt(sc), buildDoubtPrompt(topic, question, sources), onDelta)
	if err != nil {
		return nil, fmt.Errorf("answer doubt: %w", err)
	}
	return &AskResult{Answer: strings.TrimSpace(full), Sources: sources, Grounded: len(sources) > 0}, nil
}

// Probe verifies the configured AI keys with a tiny round-trip: one embedding
// (Voyage) and one short completion (Claude). Used by the admin "Test AI" button
// so keys can be validated before books are ingested. Returns a clear error if
// either provider rejects the request.
func (s *TutorService) Probe(ctx context.Context) error {
	if _, err := s.embedder.EmbedQuery(ctx, "test"); err != nil {
		return fmt.Errorf("embeddings: %w", err)
	}
	// The Chat client routes to the admin-selected answers provider
	// (Claude or Gemini), so this probes whichever one is active.
	if _, err := s.chat.Complete(ctx, "You are a test.", "Reply with the single word OK."); err != nil {
		return fmt.Errorf("answers: %w", err)
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
- Never mention "passages", "context", "chunks", or that you were given excerpts — just teach.`+languageRule(mediumOrDefault(sc.Medium), lang),
		sc.Class, group, boardOrDefault(sc.Board), mediumOrDefault(sc.Medium), lang)
}

// generalTutorSystemPrompt is used when NO textbook is indexed for the
// student's class: same warm tutor persona, answering from general knowledge
// appropriate to the class and board syllabus instead of refusing.
func generalTutorSystemPrompt(sc StudentContext) string {
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

Answer the student's question helpfully from your own knowledge, pitched at their class level and aligned with the %s syllabus. Rules:
- Explain simply and step by step, in a warm, encouraging tone a school student can follow. Prefer %s.
- Keep it concise. Use short paragraphs or bullet points. Avoid jargon; when a technical term is needed, explain it.
- Stick to educational topics; gently steer anything else back to studies.`+languageRule(mediumOrDefault(sc.Medium), lang),
		sc.Class, group, boardOrDefault(sc.Board), mediumOrDefault(sc.Medium),
		boardOrDefault(sc.Board), lang)
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
	medium := mediumOrDefault(sc.Medium)
	return fmt.Sprintf(`You are Vaha, a friendly, patient personal tutor for an Indian school student in %s%s, `+
		`%s board, studying in %s medium. You are TEACHING one homework task, step by step, like a caring teacher sitting next to the student.

BILINGUAL TEACHING METHOD (very important):
- The student's CONTENT medium is %s and their EXPLANATION language is %s.
- Present each piece of study content (definitions, sentences, formulas, examples) in %s — exactly as it would appear in their book.
- Then immediately EXPLAIN that line/concept in %s, simply, so the student truly understands it. If the two languages are the same, just teach clearly in that language.
- Example of the style (English content, Tamil explanation):
  "Photosynthesis is the process by which green plants prepare their own food."
  → Photosynthesis என்றால் பச்சைத் தாவரங்கள் தங்களுக்குத் தேவையான உணவைத் தாங்களே தயாரிக்கும் செயல்முறை.

Teach the task so the student truly understands it. Rules:
- Structure: one line on "what this is about", then the content line-by-line with its explanation (the bilingual method above), then ONE short worked example explained step by step, then a one-line "quick check" question at the end.
- NEVER give only the final answer — always show the steps and the WHY.
- Lean on the textbook passages provided when they're relevant; you may add gentle, standard, age-appropriate explanation, but do NOT introduce facts that contradict the textbook.
- Warm and encouraging. Short paragraphs or bullets. Explain any technical term.
- Never mention "passages", "context", "chunks" or that you were given excerpts — just teach.`+languageRule(medium, lang),
		sc.Class, group, boardOrDefault(sc.Board), medium,
		medium, lang, medium, lang)
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
	medium := mediumOrDefault(sc.Medium)
	return fmt.Sprintf(`You are Vaha, a friendly, patient tutor for an Indian school student in %s, `+
		`%s board, %s medium. The student is working on a homework task and has a DOUBT.

Answer the doubt clearly and simply. Rules:
- Explain in %s, in a warm, encouraging tone the student can follow. Keep any study content (definitions, formulas, book sentences) in %s and explain each in %s.
- Answer step by step — never just the final answer.
- Use the textbook passages when they help; you may add gentle standard explanation, but never contradict the textbook or invent facts.
- Keep it focused on the doubt. Give a small example if it helps.
- Never mention "passages", "context" or that you were given excerpts — just answer.`+languageRule(medium, lang),
		sc.Class, boardOrDefault(sc.Board), medium, lang, medium, lang)
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

// languageRule is the MANDATORY language instruction appended to every tutor
// prompt. Small/fast models ignore soft phrasing like "prefer Tamil", so this
// is explicit, repeated, and placed at the end of the system prompt (where
// models weigh instructions most heavily).
func languageRule(medium, lang string) string {
	if lang == "" {
		lang = medium
	}
	if strings.EqualFold(strings.TrimSpace(medium), strings.TrimSpace(lang)) {
		return fmt.Sprintf(`

LANGUAGE RULE (MANDATORY): Write your ENTIRE reply in %s, using simple words a school student understands.`, lang)
	}
	return fmt.Sprintf(`

LANGUAGE RULE (MANDATORY — the student chose this and cannot understand otherwise):
- EVERY sentence of YOUR explanation MUST be written in %s. Never write explanations in %s or any other language.
- Only the study content itself — book sentences, definitions, formulas, technical terms — stays in %s; immediately after each one, explain it in %s.
- Before you answer, check: is every explanation sentence in %s? If not, rewrite it in %s.`,
		lang, medium, medium, lang, lang, lang)
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
