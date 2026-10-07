package config

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3/option"
	"github.com/spf13/viper"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
)

type ForgeConfig struct {
	Client         string         `mapstructure:"client"`
	SpecificConfig map[string]any `mapstructure:",remain"`
}

type ProviderConfig struct {
	Client string `mapstructure:"client"`
	URL    string `mapstructure:"url"`
	Key    string `mapstructure:"key"`
}

type ModelConfig struct {
	Provider         string         `mapstructure:"provider"`
	Name             string         `mapstructure:"name"`
	AdditionalParams map[string]any `mapstructure:"additional_params"`
}

type WorkflowConfig map[string]ActionConfig

type ReviewConfig struct {
	SystemPrompt            string         `mapstructure:"system_prompt"`
	GuidelinePaths          []string       `mapstructure:"guideline_paths"`
	UncachedInputTokenLimit uint64         `mapstructure:"uncached_input_token_limit"`
	Workflow                WorkflowConfig `mapstructure:"workflow"`
}

type ActionConfig struct {
	Action         string         `mapstructure:"action"`
	InputFrom      *[]string      `mapstructure:"input_from"`
	SpecificConfig map[string]any `mapstructure:",remain"`
}

type Config struct {
	Forge     ForgeConfig               `mapstructure:"forge"`
	Providers map[string]ProviderConfig `mapstructure:"providers"`
	Models    map[string]ModelConfig    `mapstructure:"models"`
	Review    ReviewConfig              `mapstructure:"review"`
}

func LoadConfig() (*Config, error) {
	fileContents, err := os.ReadFile("gitgut.json")
	if err != nil {
		return nil, err
	}

	viperConfig := viper.New()
	viperConfig.SetConfigType("json")

	err = viperConfig.ReadConfig(bytes.NewBuffer(fileContents))
	if err != nil {
		return nil, err
	}

	var config Config
	err = viperConfig.Unmarshal(&config)
	if err != nil {
		return nil, err
	}

	// Check if model references to providers are consistent
	for modelName, model := range config.Models {
		if _, ok := config.Providers[model.Provider]; !ok {
			return nil, fmt.Errorf("config error: could not find provider model '%s' is referencing undefined provider '%s'", modelName, model.Provider)
		}
	}

	return &config, nil
}

func (c *Config) GetModel(name string) (model.LLM, error) {
	modelConfig, ok := c.Models[name]
	if !ok {
		return nil, fmt.Errorf("model '%s' does not exist in config", name)
	}

	requestOptions := make([]option.RequestOption, 0)

	for key, value := range modelConfig.AdditionalParams {
		requestOptions = append(requestOptions, option.WithJSONSet(
			key,
			value,
		))
	}

	model, err := openaimodel.NewModel(
		context.Background(),
		modelConfig.Name,
		&openaimodel.ClientConfig{
			API:     openaimodel.APIChatCompletions,
			APIKey:  c.Providers[modelConfig.Provider].Key,
			BaseURL: c.Providers[modelConfig.Provider].URL,
			Options: requestOptions,
		},
	)
	if err != nil {
		return nil, err
	}

	return model, nil
}
