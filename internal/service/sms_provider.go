package service

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
)

// SMSProvider returns a sms.ConfigFunc reading SMS settings from the DB. When
// SMS is not enabled it returns an empty (unusable) config.
func SMSProvider(settings *repository.SettingRepository) sms.ConfigFunc {
	return smsProvider(settings, false)
}

// SMSProviderForce ignores the "enabled" toggle — returns the configured
// provider whenever its credentials are present. Used by the "test SMS".
func SMSProviderForce(settings *repository.SettingRepository) sms.ConfigFunc {
	return smsProvider(settings, true)
}

func smsProvider(settings *repository.SettingRepository, ignoreEnabled bool) sms.ConfigFunc {
	return func() sms.Config {
		s, err := settings.Get()
		if err != nil {
			return sms.Config{}
		}
		if !ignoreEnabled && !s.SmsEnabled {
			return sms.Config{}
		}
		provider := s.SmsProvider
		if provider == "" {
			provider = sms.ProviderNexmo
		}
		cc := s.SmsCountryCode
		if cc == "" {
			cc = "91"
		}
		return sms.Config{
			Provider:    provider,
			CountryCode: cc,
			Nexmo: sms.NexmoConfig{
				APIKey:    s.NexmoAPIKey,
				APISecret: s.NexmoAPISecret,
				From:      s.NexmoFrom,
			},
			SmsExpert: sms.SmsExpertConfig{
				APIURL:   s.SmsExpertAPIURL,
				User:     s.SmsExpertUser,
				Password: s.SmsExpertPassword,
				From:     s.SmsExpertSender,
				Route:    s.SmsExpertRoute,
				Type:     s.SmsExpertType,
			},
		}
	}
}
