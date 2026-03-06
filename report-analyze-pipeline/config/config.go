package config

import (
	"cleanapp-common/appenv"
	"fmt"
	"strings"
	"time"
)

var languageCodeMap = map[string]string{
	"en": "English",
	"me": "Montenegrin",
	"de": "German",
}

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	Port            string
	RunDBMigrations bool

	OpenAIAPIKey      string
	OpenAIAssistantID string
	OpenAIModel       string
	GeminiAPIKey      string
	GeminiModel       string
	LLMProvider       string

	AnalysisInterval time.Duration
	MaxRetries       int
	AnalysisPrompt   string

	TranslationLanguages map[string]string
	LogLevel             string
	SeqStartFrom         int
	RabbitMQ             RabbitMQConfig
}

type RabbitMQConfig struct {
	Host                     string
	Port                     string
	User                     string
	Password                 string
	Exchange                 string
	Queue                    string
	RawReportRoutingKey      string
	AnalysedReportRoutingKey string
	PrefetchCount            int
}

func (r *RabbitMQConfig) GetAMQPURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%s/", r.User, r.Password, r.Host, r.Port)
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	amqpUser, err := appenv.StringRequiredInProd("AMQP_USER", "")
	if err != nil {
		return nil, err
	}
	amqpPassword, err := appenv.Secret("AMQP_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	config := &Config{
		DBHost:               appenv.String("DB_HOST", "localhost"),
		DBPort:               appenv.String("DB_PORT", "3306"),
		DBUser:               appenv.String("DB_USER", "server"),
		DBPassword:           dbPassword,
		DBName:               appenv.String("DB_NAME", "cleanapp"),
		RunDBMigrations:      appenv.Bool("DB_RUN_MIGRATIONS", appenv.DefaultRunMigrations()),
		Port:                 appenv.String("PORT", "8080"),
		OpenAIAPIKey:         appenv.String("OPENAI_API_KEY", ""),
		OpenAIAssistantID:    appenv.String("OPENAI_ASSISTANT_ID", ""),
		OpenAIModel:          appenv.String("OPENAI_MODEL", "gpt-4o"),
		GeminiAPIKey:         appenv.String("GEMINI_API_KEY", ""),
		GeminiModel:          appenv.String("GEMINI_MODEL", "gemini-flash-latest"),
		LLMProvider:          appenv.String("ANALYZER_LLM_PROVIDER", "openai"),
		AnalysisInterval:     appenv.Duration("ANALYSIS_INTERVAL", 30*time.Second),
		MaxRetries:           appenv.Int("MAX_RETRIES", 3),
		TranslationLanguages: getLanguageMapEnv("TRANSLATION_LANGUAGES", "en"),
		LogLevel:             appenv.String("LOG_LEVEL", "info"),
		SeqStartFrom:         appenv.Int("SEQ_START_FROM", 0),
		RabbitMQ: RabbitMQConfig{
			Host:                     appenv.String("AMQP_HOST", "localhost"),
			Port:                     appenv.String("AMQP_PORT", "5672"),
			User:                     amqpUser,
			Password:                 amqpPassword,
			Exchange:                 appenv.String("RABBITMQ_EXCHANGE", "cleanapp"),
			Queue:                    appenv.String("RABBITMQ_QUEUE", "report-analyze"),
			RawReportRoutingKey:      appenv.String("RABBITMQ_RAW_REPORT_ROUTING_KEY", "report.raw"),
			AnalysedReportRoutingKey: appenv.String("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY", "report.analysed"),
			PrefetchCount:            appenv.Int("RABBITMQ_PREFETCH_COUNT", 5),
		},
	}
	return config, nil
}

func getLanguageMapEnv(key, defaultValue string) map[string]string {
	value := appenv.String(key, defaultValue)
	if value == "" {
		return map[string]string{}
	}

	codes := strings.Split(value, ",")
	languages := make(map[string]string)
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if fullName, exists := languageCodeMap[code]; exists {
			languages[code] = fullName
		} else {
			languages[code] = code
		}
	}
	return languages
}
