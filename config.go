package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type globalConfig struct {
	// GoogleAPIKeys is preferred (supports rotation). Limited to 10 keys to match runtime behavior.
	GoogleAPIKeys []string `json:"google_api_keys,omitempty"`
	// GoogleAPIKey is kept for backward compatibility with older config files.
	GoogleAPIKey string `json:"google_api_key,omitempty"`
}

func globalConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "parfait", "config.json"), nil
}

func normalizeKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func loadGlobalConfig() (globalConfig, error) {
	p, err := globalConfigPath()
	if err != nil {
		return globalConfig{}, err
	}

	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return globalConfig{}, nil
		}
		return globalConfig{}, err
	}

	var cfg globalConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return globalConfig{}, fmt.Errorf("invalid config file (%s): %w", p, err)
	}

	// Migrate legacy single key into keys list (in-memory).
	if len(cfg.GoogleAPIKeys) == 0 && strings.TrimSpace(cfg.GoogleAPIKey) != "" {
		cfg.GoogleAPIKeys = []string{strings.TrimSpace(cfg.GoogleAPIKey)}
	}
	cfg.GoogleAPIKeys = normalizeKeys(cfg.GoogleAPIKeys)
	return cfg, nil
}

func saveGlobalConfig(cfg globalConfig) error {
	p, err := globalConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	// Best-effort: 0600 for unix-like. Windows ignores permissions.
	return os.WriteFile(p, b, 0o600)
}

// applyGlobalEnvDefaults loads global config and sets env vars only if they are not already set.
func applyGlobalEnvDefaults() error {
	cfg, err := loadGlobalConfig()
	if err != nil {
		return err
	}

	// Only set default if process env doesn't already define any key.
	hasAnyKey := os.Getenv("GOOGLE_API_KEY") != ""
	if !hasAnyKey {
		for i := 1; i <= 10; i++ {
			if os.Getenv(fmt.Sprintf("GOOGLE_API_KEY_%d", i)) != "" {
				hasAnyKey = true
				break
			}
		}
	}

	if !hasAnyKey && len(cfg.GoogleAPIKeys) > 0 {
		// Prefer numbered keys for rotation behavior.
		for i, k := range cfg.GoogleAPIKeys {
			if i >= 10 {
				break
			}
			_ = os.Setenv(fmt.Sprintf("GOOGLE_API_KEY_%d", i+1), k)
		}
		// Also set GOOGLE_API_KEY for compatibility if not already present.
		_ = os.Setenv("GOOGLE_API_KEY", cfg.GoogleAPIKeys[0])
	}

	return nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage global configuration",
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print global config file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := globalConfigPath()
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), p)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a config value",
}

var configSetAPIKeyCmd = &cobra.Command{
	Use:   "api-key <KEY>",
	Short: "Set Google Gemini API key globally (replace all keys)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := strings.TrimSpace(args[0])
		if key == "" {
			return fmt.Errorf("api key is empty")
		}

		cfg, err := loadGlobalConfig()
		if err != nil {
			return err
		}
		cfg.GoogleAPIKeys = []string{key}
		cfg.GoogleAPIKey = "" // legacy field no longer needed
		if err := saveGlobalConfig(cfg); err != nil {
			return err
		}

		p, _ := globalConfigPath()
		fmt.Fprintf(cmd.OutOrStdout(), "Saved 1 api key to %s\n", p)
		return nil
	},
}

var configAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a config value",
}

var configAddAPIKeyCmd = &cobra.Command{
	Use:   "api-key <KEY>",
	Short: "Add Google Gemini API key globally (append for rotation)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := strings.TrimSpace(args[0])
		if key == "" {
			return fmt.Errorf("api key is empty")
		}

		cfg, err := loadGlobalConfig()
		if err != nil {
			return err
		}
		cfg.GoogleAPIKeys = append(cfg.GoogleAPIKeys, key)
		cfg.GoogleAPIKeys = normalizeKeys(cfg.GoogleAPIKeys)
		if len(cfg.GoogleAPIKeys) > 10 {
			return fmt.Errorf("too many api keys: %d (max 10)", len(cfg.GoogleAPIKeys))
		}
		cfg.GoogleAPIKey = "" // legacy field no longer needed

		if err := saveGlobalConfig(cfg); err != nil {
			return err
		}

		p, _ := globalConfigPath()
		fmt.Fprintf(cmd.OutOrStdout(), "Saved %d api key(s) to %s\n", len(cfg.GoogleAPIKeys), p)
		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List config values",
}

var configListAPIKeysCmd = &cobra.Command{
	Use:   "api-keys",
	Short: "List saved Gemini API keys (masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadGlobalConfig()
		if err != nil {
			return err
		}
		if len(cfg.GoogleAPIKeys) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no api keys set)")
			return nil
		}
		for i, k := range cfg.GoogleAPIKeys {
			masked := k
			if len(masked) > 8 {
				masked = masked[:4] + "..." + masked[len(masked)-4:]
			} else {
				masked = "****"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d: %s\n", i+1, masked)
		}
		return nil
	},
}

func init() {
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configSetCmd)
	configSetCmd.AddCommand(configSetAPIKeyCmd)
	configCmd.AddCommand(configAddCmd)
	configAddCmd.AddCommand(configAddAPIKeyCmd)
	configCmd.AddCommand(configListCmd)
	configListCmd.AddCommand(configListAPIKeysCmd)
}
