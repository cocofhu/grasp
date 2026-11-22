package database

import (
	"slices"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// legacyCursorModel is where an id of the cursor-agent catalog, which baked
// the reasoning level and Fast into the id, lands in the @cursor/sdk catalog:
// the base model plus a separate effort and speed tier.
type legacyCursorModel struct{ base, effort, tier string }

// legacyCursorModels covers every id the cursor-agent catalog offered that is
// not already a base model. It is frozen: SDK ids never need an entry. Thinking
// and non-thinking Claude ids share a base, which runs Cursor's default.
var legacyCursorModels = map[string]legacyCursorModel{
	"gpt-5.3-codex-low":                    {"gpt-5.3-codex", "low", ""},
	"gpt-5.3-codex-low-fast":               {"gpt-5.3-codex", "low", "fast"},
	"gpt-5.3-codex-fast":                   {"gpt-5.3-codex", "", "fast"},
	"gpt-5.3-codex-high":                   {"gpt-5.3-codex", "high", ""},
	"gpt-5.3-codex-high-fast":              {"gpt-5.3-codex", "high", "fast"},
	"gpt-5.3-codex-xhigh":                  {"gpt-5.3-codex", "xhigh", ""},
	"gpt-5.3-codex-xhigh-fast":             {"gpt-5.3-codex", "xhigh", "fast"},
	"claude-opus-5-thinking-high":          {"claude-opus-5", "high", ""},
	"claude-opus-5-thinking-high-fast":     {"claude-opus-5", "high", "fast"},
	"gpt-5.6-sol-high":                     {"gpt-5.6-sol", "high", ""},
	"gpt-5.6-sol-high-fast":                {"gpt-5.6-sol", "high", "fast"},
	"gpt-5.6-sol-xhigh":                    {"gpt-5.6-sol", "xhigh", ""},
	"gpt-5.6-sol-xhigh-fast":               {"gpt-5.6-sol", "xhigh", "fast"},
	"claude-fable-5-thinking-high":         {"claude-fable-5", "high", ""},
	"claude-fable-5-thinking-xhigh":        {"claude-fable-5", "xhigh", ""},
	"cursor-grok-4.5-high":                 {"grok-4.5", "high", ""},
	"cursor-grok-4.5-high-fast":            {"grok-4.5", "high", "fast"},
	"claude-sonnet-5-thinking-high":        {"claude-sonnet-5", "high", ""},
	"claude-sonnet-5-thinking-xhigh":       {"claude-sonnet-5", "xhigh", ""},
	"gpt-5.6-luna-high":                    {"gpt-5.6-luna", "high", ""},
	"grok-4.7-low":                         {"grok-4.7", "low", ""},
	"grok-4.7-low-fast":                    {"grok-4.7", "low", "fast"},
	"grok-4.7-medium":                      {"grok-4.7", "medium", ""},
	"grok-4.7-medium-fast":                 {"grok-4.7", "medium", "fast"},
	"grok-4.7-high":                        {"grok-4.7", "high", ""},
	"grok-4.7-high-fast":                   {"grok-4.7", "high", "fast"},
	"grok-4.7-xhigh":                       {"grok-4.7", "xhigh", ""},
	"grok-4.7-xhigh-fast":                  {"grok-4.7", "xhigh", "fast"},
	"cursor-grok-4.6-low":                  {"grok-4.6", "low", ""},
	"cursor-grok-4.6-low-fast":             {"grok-4.6", "low", "fast"},
	"cursor-grok-4.6-medium":               {"grok-4.6", "medium", ""},
	"cursor-grok-4.6-medium-fast":          {"grok-4.6", "medium", "fast"},
	"cursor-grok-4.6-high":                 {"grok-4.6", "high", ""},
	"cursor-grok-4.6-high-fast":            {"grok-4.6", "high", "fast"},
	"cursor-grok-4.6-xhigh":                {"grok-4.6", "xhigh", ""},
	"cursor-grok-4.6-xhigh-fast":           {"grok-4.6", "xhigh", "fast"},
	"composer-2.5-fast":                    {"composer-2.5", "", "fast"},
	"claude-opus-5-5-low":                  {"claude-opus-5-5", "low", ""},
	"claude-opus-5-5-low-fast":             {"claude-opus-5-5", "low", "fast"},
	"claude-opus-5-5-medium":               {"claude-opus-5-5", "medium", ""},
	"claude-opus-5-5-medium-fast":          {"claude-opus-5-5", "medium", "fast"},
	"claude-opus-5-5-high":                 {"claude-opus-5-5", "high", ""},
	"claude-opus-5-5-high-fast":            {"claude-opus-5-5", "high", "fast"},
	"claude-opus-5-5-xhigh":                {"claude-opus-5-5", "xhigh", ""},
	"claude-opus-5-5-xhigh-fast":           {"claude-opus-5-5", "xhigh", "fast"},
	"claude-opus-5-5-max":                  {"claude-opus-5-5", "max", ""},
	"claude-opus-5-5-max-fast":             {"claude-opus-5-5", "max", "fast"},
	"claude-opus-5-low":                    {"claude-opus-5", "low", ""},
	"claude-opus-5-low-fast":               {"claude-opus-5", "low", "fast"},
	"claude-opus-5-medium":                 {"claude-opus-5", "medium", ""},
	"claude-opus-5-medium-fast":            {"claude-opus-5", "medium", "fast"},
	"claude-opus-5-high":                   {"claude-opus-5", "high", ""},
	"claude-opus-5-high-fast":              {"claude-opus-5", "high", "fast"},
	"claude-opus-5-thinking-low":           {"claude-opus-5", "low", ""},
	"claude-opus-5-thinking-low-fast":      {"claude-opus-5", "low", "fast"},
	"claude-opus-5-thinking-medium":        {"claude-opus-5", "medium", ""},
	"claude-opus-5-thinking-medium-fast":   {"claude-opus-5", "medium", "fast"},
	"claude-opus-5-thinking-xhigh":         {"claude-opus-5", "xhigh", ""},
	"claude-opus-5-thinking-xhigh-fast":    {"claude-opus-5", "xhigh", "fast"},
	"claude-opus-5-thinking-max":           {"claude-opus-5", "max", ""},
	"claude-opus-5-thinking-max-fast":      {"claude-opus-5", "max", "fast"},
	"claude-opus-4-8-low":                  {"claude-opus-4-8", "low", ""},
	"claude-opus-4-8-low-fast":             {"claude-opus-4-8", "low", "fast"},
	"claude-opus-4-8-medium":               {"claude-opus-4-8", "medium", ""},
	"claude-opus-4-8-medium-fast":          {"claude-opus-4-8", "medium", "fast"},
	"claude-opus-4-8-high":                 {"claude-opus-4-8", "high", ""},
	"claude-opus-4-8-high-fast":            {"claude-opus-4-8", "high", "fast"},
	"claude-opus-4-8-xhigh":                {"claude-opus-4-8", "xhigh", ""},
	"claude-opus-4-8-xhigh-fast":           {"claude-opus-4-8", "xhigh", "fast"},
	"claude-opus-4-8-max":                  {"claude-opus-4-8", "max", ""},
	"claude-opus-4-8-max-fast":             {"claude-opus-4-8", "max", "fast"},
	"claude-opus-4-8-thinking-low":         {"claude-opus-4-8", "low", ""},
	"claude-opus-4-8-thinking-low-fast":    {"claude-opus-4-8", "low", "fast"},
	"claude-opus-4-8-thinking-medium":      {"claude-opus-4-8", "medium", ""},
	"claude-opus-4-8-thinking-medium-fast": {"claude-opus-4-8", "medium", "fast"},
	"claude-opus-4-8-thinking-high":        {"claude-opus-4-8", "high", ""},
	"claude-opus-4-8-thinking-high-fast":   {"claude-opus-4-8", "high", "fast"},
	"claude-opus-4-8-thinking-xhigh":       {"claude-opus-4-8", "xhigh", ""},
	"claude-opus-4-8-thinking-xhigh-fast":  {"claude-opus-4-8", "xhigh", "fast"},
	"claude-opus-4-8-thinking-max":         {"claude-opus-4-8", "max", ""},
	"claude-opus-4-8-thinking-max-fast":    {"claude-opus-4-8", "max", "fast"},
	"gemini-4-carbon-low":                  {"gemini-4-carbon", "low", ""},
	"gemini-4-carbon-medium":               {"gemini-4-carbon", "medium", ""},
	"gemini-4-carbon-high":                 {"gemini-4-carbon", "high", ""},
	"gemini-4-hydrogen-low":                {"gemini-4-hydrogen", "low", ""},
	"gemini-4-hydrogen-medium":             {"gemini-4-hydrogen", "medium", ""},
	"gemini-4-hydrogen-high":               {"gemini-4-hydrogen", "high", ""},
	"gemini-4-hydrogen-xhigh":              {"gemini-4-hydrogen", "xhigh", ""},
	"gemini-4-argon-low":                   {"gemini-4-argon", "low", ""},
	"gemini-4-argon-medium":                {"gemini-4-argon", "medium", ""},
	"gemini-4-argon-high":                  {"gemini-4-argon", "high", ""},
	"muse-wonder-1.0-minimal":              {"muse-wonder-1.0", "minimal", ""},
	"muse-wonder-1.0-low":                  {"muse-wonder-1.0", "low", ""},
	"muse-wonder-1.0-medium":               {"muse-wonder-1.0", "medium", ""},
	"muse-wonder-1.0-high":                 {"muse-wonder-1.0", "high", ""},
	"muse-wonder-1.0-xhigh":                {"muse-wonder-1.0", "xhigh", ""},
	"muse-wonder-1.0-max":                  {"muse-wonder-1.0", "max", ""},
	"gpt-5.6-sol-none":                     {"gpt-5.6-sol", "none", ""},
	"gpt-5.6-sol-none-fast":                {"gpt-5.6-sol", "none", "fast"},
	"gpt-5.6-sol-low":                      {"gpt-5.6-sol", "low", ""},
	"gpt-5.6-sol-low-fast":                 {"gpt-5.6-sol", "low", "fast"},
	"gpt-5.6-sol-medium":                   {"gpt-5.6-sol", "medium", ""},
	"gpt-5.6-sol-medium-fast":              {"gpt-5.6-sol", "medium", "fast"},
	"gpt-5.6-sol-max":                      {"gpt-5.6-sol", "max", ""},
	"gpt-5.6-sol-max-fast":                 {"gpt-5.6-sol", "max", "fast"},
	"gpt-5.5-none":                         {"gpt-5.5", "none", ""},
	"gpt-5.5-none-fast":                    {"gpt-5.5", "none", "fast"},
	"gpt-5.5-low":                          {"gpt-5.5", "low", ""},
	"gpt-5.5-low-fast":                     {"gpt-5.5", "low", "fast"},
	"gpt-5.5-medium":                       {"gpt-5.5", "medium", ""},
	"gpt-5.5-medium-fast":                  {"gpt-5.5", "medium", "fast"},
	"gpt-5.5-high":                         {"gpt-5.5", "high", ""},
	"gpt-5.5-high-fast":                    {"gpt-5.5", "high", "fast"},
	"gpt-5.5-extra-high":                   {"gpt-5.5", "xhigh", ""},
	"gpt-5.5-extra-high-fast":              {"gpt-5.5", "xhigh", "fast"},
	"claude-fable-5-2-low":                 {"claude-fable-5-2", "low", ""},
	"claude-fable-5-2-low-fast":            {"claude-fable-5-2", "low", "fast"},
	"claude-fable-5-2-medium":              {"claude-fable-5-2", "medium", ""},
	"claude-fable-5-2-medium-fast":         {"claude-fable-5-2", "medium", "fast"},
	"claude-fable-5-2-high":                {"claude-fable-5-2", "high", ""},
	"claude-fable-5-2-high-fast":           {"claude-fable-5-2", "high", "fast"},
	"claude-fable-5-2-xhigh":               {"claude-fable-5-2", "xhigh", ""},
	"claude-fable-5-2-xhigh-fast":          {"claude-fable-5-2", "xhigh", "fast"},
	"claude-fable-5-2-max":                 {"claude-fable-5-2", "max", ""},
	"claude-fable-5-2-max-fast":            {"claude-fable-5-2", "max", "fast"},
	"claude-fable-5-1-low":                 {"claude-fable-5-1", "low", ""},
	"claude-fable-5-1-medium":              {"claude-fable-5-1", "medium", ""},
	"claude-fable-5-1-high":                {"claude-fable-5-1", "high", ""},
	"claude-fable-5-1-xhigh":               {"claude-fable-5-1", "xhigh", ""},
	"claude-fable-5-1-max":                 {"claude-fable-5-1", "max", ""},
	"claude-fable-5-1-thinking-low":        {"claude-fable-5-1", "low", ""},
	"claude-fable-5-1-thinking-medium":     {"claude-fable-5-1", "medium", ""},
	"claude-fable-5-1-thinking-high":       {"claude-fable-5-1", "high", ""},
	"claude-fable-5-1-thinking-xhigh":      {"claude-fable-5-1", "xhigh", ""},
	"claude-fable-5-1-thinking-max":        {"claude-fable-5-1", "max", ""},
	"claude-fable-5-low":                   {"claude-fable-5", "low", ""},
	"claude-fable-5-medium":                {"claude-fable-5", "medium", ""},
	"claude-fable-5-high":                  {"claude-fable-5", "high", ""},
	"claude-fable-5-xhigh":                 {"claude-fable-5", "xhigh", ""},
	"claude-fable-5-max":                   {"claude-fable-5", "max", ""},
	"claude-fable-5-thinking-low":          {"claude-fable-5", "low", ""},
	"claude-fable-5-thinking-medium":       {"claude-fable-5", "medium", ""},
	"claude-fable-5-thinking-max":          {"claude-fable-5", "max", ""},
	"cursor-grok-4.5-low":                  {"grok-4.5", "low", ""},
	"cursor-grok-4.5-low-fast":             {"grok-4.5", "low", "fast"},
	"cursor-grok-4.5-medium":               {"grok-4.5", "medium", ""},
	"cursor-grok-4.5-medium-fast":          {"grok-4.5", "medium", "fast"},
	"gemini-3.8-flash-low":                 {"gemini-3.8-flash", "low", ""},
	"gemini-3.8-flash-medium":              {"gemini-3.8-flash", "medium", ""},
	"gemini-3.8-flash-high":                {"gemini-3.8-flash", "high", ""},
	"muse-spark-1.3-minimal":               {"muse-spark-1.3", "minimal", ""},
	"muse-spark-1.3-low":                   {"muse-spark-1.3", "low", ""},
	"muse-spark-1.3-medium":                {"muse-spark-1.3", "medium", ""},
	"muse-spark-1.3-high":                  {"muse-spark-1.3", "high", ""},
	"muse-spark-1.3-xhigh":                 {"muse-spark-1.3", "xhigh", ""},
	"muse-spark-1.3-max":                   {"muse-spark-1.3", "max", ""},
	"gpt-5.6-terra-none":                   {"gpt-5.6-terra", "none", ""},
	"gpt-5.6-terra-none-fast":              {"gpt-5.6-terra", "none", "fast"},
	"gpt-5.6-terra-low":                    {"gpt-5.6-terra", "low", ""},
	"gpt-5.6-terra-low-fast":               {"gpt-5.6-terra", "low", "fast"},
	"gpt-5.6-terra-medium":                 {"gpt-5.6-terra", "medium", ""},
	"gpt-5.6-terra-medium-fast":            {"gpt-5.6-terra", "medium", "fast"},
	"gpt-5.6-terra-high":                   {"gpt-5.6-terra", "high", ""},
	"gpt-5.6-terra-high-fast":              {"gpt-5.6-terra", "high", "fast"},
	"gpt-5.6-terra-xhigh":                  {"gpt-5.6-terra", "xhigh", ""},
	"gpt-5.6-terra-xhigh-fast":             {"gpt-5.6-terra", "xhigh", "fast"},
	"gpt-5.6-terra-max":                    {"gpt-5.6-terra", "max", ""},
	"gpt-5.6-terra-max-fast":               {"gpt-5.6-terra", "max", "fast"},
	"claude-sonnet-5-5-low":                {"claude-sonnet-5-5", "low", ""},
	"claude-sonnet-5-5-medium":             {"claude-sonnet-5-5", "medium", ""},
	"claude-sonnet-5-5-high":               {"claude-sonnet-5-5", "high", ""},
	"claude-sonnet-5-5-xhigh":              {"claude-sonnet-5-5", "xhigh", ""},
	"claude-sonnet-5-5-max":                {"claude-sonnet-5-5", "max", ""},
	"claude-sonnet-5-low":                  {"claude-sonnet-5", "low", ""},
	"claude-sonnet-5-medium":               {"claude-sonnet-5", "medium", ""},
	"claude-sonnet-5-high":                 {"claude-sonnet-5", "high", ""},
	"claude-sonnet-5-xhigh":                {"claude-sonnet-5", "xhigh", ""},
	"claude-sonnet-5-max":                  {"claude-sonnet-5", "max", ""},
	"claude-sonnet-5-thinking-low":         {"claude-sonnet-5", "low", ""},
	"claude-sonnet-5-thinking-medium":      {"claude-sonnet-5", "medium", ""},
	"claude-sonnet-5-thinking-max":         {"claude-sonnet-5", "max", ""},
	"claude-4.6-sonnet-medium":             {"claude-sonnet-4-6", "medium", ""},
	"claude-4.6-sonnet-medium-thinking":    {"claude-sonnet-4-6", "medium", ""},
	"claude-opus-4-7-low":                  {"claude-opus-4-7", "low", ""},
	"claude-opus-4-7-low-fast":             {"claude-opus-4-7", "low", "fast"},
	"claude-opus-4-7-medium":               {"claude-opus-4-7", "medium", ""},
	"claude-opus-4-7-medium-fast":          {"claude-opus-4-7", "medium", "fast"},
	"claude-opus-4-7-high":                 {"claude-opus-4-7", "high", ""},
	"claude-opus-4-7-high-fast":            {"claude-opus-4-7", "high", "fast"},
	"claude-opus-4-7-xhigh":                {"claude-opus-4-7", "xhigh", ""},
	"claude-opus-4-7-xhigh-fast":           {"claude-opus-4-7", "xhigh", "fast"},
	"claude-opus-4-7-max":                  {"claude-opus-4-7", "max", ""},
	"claude-opus-4-7-max-fast":             {"claude-opus-4-7", "max", "fast"},
	"claude-opus-4-7-thinking-low":         {"claude-opus-4-7", "low", ""},
	"claude-opus-4-7-thinking-low-fast":    {"claude-opus-4-7", "low", "fast"},
	"claude-opus-4-7-thinking-medium":      {"claude-opus-4-7", "medium", ""},
	"claude-opus-4-7-thinking-medium-fast": {"claude-opus-4-7", "medium", "fast"},
	"claude-opus-4-7-thinking-high":        {"claude-opus-4-7", "high", ""},
	"claude-opus-4-7-thinking-high-fast":   {"claude-opus-4-7", "high", "fast"},
	"claude-opus-4-7-thinking-xhigh":       {"claude-opus-4-7", "xhigh", ""},
	"claude-opus-4-7-thinking-xhigh-fast":  {"claude-opus-4-7", "xhigh", "fast"},
	"claude-opus-4-7-thinking-max":         {"claude-opus-4-7", "max", ""},
	"claude-opus-4-7-thinking-max-fast":    {"claude-opus-4-7", "max", "fast"},
	"gpt-5.4-low":                          {"gpt-5.4", "low", ""},
	"gpt-5.4-medium":                       {"gpt-5.4", "medium", ""},
	"gpt-5.4-medium-fast":                  {"gpt-5.4", "medium", "fast"},
	"gpt-5.4-high":                         {"gpt-5.4", "high", ""},
	"gpt-5.4-high-fast":                    {"gpt-5.4", "high", "fast"},
	"gpt-5.4-xhigh":                        {"gpt-5.4", "xhigh", ""},
	"gpt-5.4-xhigh-fast":                   {"gpt-5.4", "xhigh", "fast"},
	"claude-4.6-opus-high":                 {"claude-opus-4-6", "high", ""},
	"claude-4.6-opus-max":                  {"claude-opus-4-6", "max", ""},
	"claude-4.6-opus-high-thinking":        {"claude-opus-4-6", "high", ""},
	"claude-4.6-opus-max-thinking":         {"claude-opus-4-6", "max", ""},
	"claude-4.5-opus-high":                 {"claude-opus-4-5", "", ""},
	"claude-4.5-opus-high-thinking":        {"claude-opus-4-5", "", ""},
	"gpt-5.2-low":                          {"gpt-5.2", "low", ""},
	"gpt-5.2-low-fast":                     {"gpt-5.2", "low", "fast"},
	"gpt-5.2-fast":                         {"gpt-5.2", "", "fast"},
	"gpt-5.2-high":                         {"gpt-5.2", "high", ""},
	"gpt-5.2-high-fast":                    {"gpt-5.2", "high", "fast"},
	"gpt-5.2-xhigh":                        {"gpt-5.2", "xhigh", ""},
	"gpt-5.2-xhigh-fast":                   {"gpt-5.2", "xhigh", "fast"},
	"gpt-5.6-luna-none":                    {"gpt-5.6-luna", "none", ""},
	"gpt-5.6-luna-none-fast":               {"gpt-5.6-luna", "none", "fast"},
	"gpt-5.6-luna-low":                     {"gpt-5.6-luna", "low", ""},
	"gpt-5.6-luna-low-fast":                {"gpt-5.6-luna", "low", "fast"},
	"gpt-5.6-luna-medium":                  {"gpt-5.6-luna", "medium", ""},
	"gpt-5.6-luna-medium-fast":             {"gpt-5.6-luna", "medium", "fast"},
	"gpt-5.6-luna-high-fast":               {"gpt-5.6-luna", "high", "fast"},
	"gpt-5.6-luna-xhigh":                   {"gpt-5.6-luna", "xhigh", ""},
	"gpt-5.6-luna-xhigh-fast":              {"gpt-5.6-luna", "xhigh", "fast"},
	"gpt-5.6-luna-max":                     {"gpt-5.6-luna", "max", ""},
	"gpt-5.6-luna-max-fast":                {"gpt-5.6-luna", "max", "fast"},
	"gpt-5.4-mini-none":                    {"gpt-5.4-mini", "none", ""},
	"gpt-5.4-mini-low":                     {"gpt-5.4-mini", "low", ""},
	"gpt-5.4-mini-medium":                  {"gpt-5.4-mini", "medium", ""},
	"gpt-5.4-mini-high":                    {"gpt-5.4-mini", "high", ""},
	"gpt-5.4-mini-xhigh":                   {"gpt-5.4-mini", "xhigh", ""},
	"gpt-5.4-nano-none":                    {"gpt-5.4-nano", "none", ""},
	"gpt-5.4-nano-low":                     {"gpt-5.4-nano", "low", ""},
	"gpt-5.4-nano-medium":                  {"gpt-5.4-nano", "medium", ""},
	"gpt-5.4-nano-high":                    {"gpt-5.4-nano", "high", ""},
	"gpt-5.4-nano-xhigh":                   {"gpt-5.4-nano", "xhigh", ""},
	"claude-haiku-5-5-low":                 {"claude-haiku-5-5", "low", ""},
	"claude-haiku-5-5-medium":              {"claude-haiku-5-5", "medium", ""},
	"claude-haiku-5-5-high":                {"claude-haiku-5-5", "high", ""},
	"claude-haiku-5-5-xhigh":               {"claude-haiku-5-5", "xhigh", ""},
	"claude-haiku-5-5-max":                 {"claude-haiku-5-5", "max", ""},
	"claude-haiku-5-5-thinking-low":        {"claude-haiku-5-5", "low", ""},
	"claude-haiku-5-5-thinking-medium":     {"claude-haiku-5-5", "medium", ""},
	"claude-haiku-5-5-thinking-high":       {"claude-haiku-5-5", "high", ""},
	"claude-haiku-5-5-thinking-xhigh":      {"claude-haiku-5-5", "xhigh", ""},
	"claude-haiku-5-5-thinking-max":        {"claude-haiku-5-5", "max", ""},
	"claude-4.5-sonnet":                    {"claude-sonnet-4-5", "", ""},
	"claude-4.5-sonnet-thinking":           {"claude-sonnet-4-5", "", ""},
	"gpt-5.1-low":                          {"gpt-5.1", "low", ""},
	"gpt-5.1-high":                         {"gpt-5.1", "high", ""},
	"claude-4-sonnet":                      {"claude-sonnet-4", "", ""},
	"claude-4-sonnet-thinking":             {"claude-sonnet-4", "", ""},
	"kimi-k3-low":                          {"kimi-k3", "low", ""},
	"kimi-k3-high":                         {"kimi-k3", "high", ""},
	"kimi-k3-max":                          {"kimi-k3", "max", ""},
	"glm-5.2-high":                         {"glm-5.2", "high", ""},
	"glm-5.2-max":                          {"glm-5.2", "max", ""},
	"deepseek-v41-flash-1017-none":         {"deepseek-v41-flash-1017", "none", ""},
	"deepseek-v41-flash-1017-low":          {"deepseek-v41-flash-1017", "low", ""},
	"deepseek-v41-flash-1017-medium":       {"deepseek-v41-flash-1017", "medium", ""},
	"deepseek-v41-flash-1017-high":         {"deepseek-v41-flash-1017", "high", ""},
	"deepseek-v41-flash-1017-max":          {"deepseek-v41-flash-1017", "max", ""},
}

// cursorRouteColumns are the configuration, model, effort and tier columns of
// one stored model choice, and the column that marks the choice explicit.
// Initial choices have no effort or tier of their own.
var cursorRouteColumns = []struct {
	table, set, configuration, model, effort, tier string
}{
	{"solo_sessions", "", "initial_configuration_id", "initial_model_id", "", ""},
	{"solo_sessions", "current_model_set", "current_configuration_id", "current_model_id", "current_reasoning_effort", "current_service_tier"},
	{"solo_sessions", "applied_model_set", "applied_configuration_id", "applied_model_id", "applied_reasoning_effort", "applied_service_tier"},
	{"react_conversations", "current_model_set", "current_configuration_id", "current_model_id", "current_reasoning_effort", "current_service_tier"},
	{"react_conversations", "applied_model_set", "applied_configuration_id", "applied_model_id", "applied_reasoning_effort", "applied_service_tier"},
	{"runs", "launch_model_set", "launch_configuration_id", "launch_model_id", "launch_reasoning_effort", "launch_service_tier"},
}

// migrateLegacyCursorModels rewrites cursor-agent model ids saved on Cursor
// configurations and on the sessions, visits and runs that use them. A choice
// keeps its level and speed: an empty effort or tier takes the one the old id
// carried, also for an explicit choice of a configuration's old primary.
// Other backends' ids are never touched, and a second pass is a no-op.
func migrateLegacyCursorModels(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&models.ModelConfiguration{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var configurations []models.ModelConfiguration
		if err := tx.Where("backend = ?", "cursor").Find(&configurations).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(configurations))
		primary := map[string]legacyCursorModel{}
		for i := range configurations {
			c := &configurations[i]
			ids = append(ids, c.ID)
			old, legacy := legacyCursorModels[strings.TrimSpace(c.Binding.ModelID)]
			if legacy {
				primary[c.ID] = old
				c.Binding.ModelID = old.base
			}
			aux, auxChanged := migrateCursorAuxiliary(c.Binding.ModelID, c.Binding.AuxiliaryModelIDs)
			if !legacy && !auxChanged {
				continue
			}
			c.Binding.AuxiliaryModelIDs = aux
			c.Revision++
			c.UpdatedAt = time.Now()
			if err := tx.Model(c).Select("binding", "revision", "updated_at").Updates(c).Error; err != nil {
				return err
			}
		}
		if len(ids) == 0 {
			return nil
		}
		for _, cols := range cursorRouteColumns {
			if err := migrateCursorRoutes(tx, ids, primary, cols.table, cols.set, cols.configuration, cols.model, cols.effort, cols.tier); err != nil {
				return err
			}
		}
		return nil
	})
}

// migrateCursorAuxiliary maps auxiliary ids onto base models, dropping the
// primary and repeats that several old ids collapse into.
func migrateCursorAuxiliary(primary string, aux []string) ([]string, bool) {
	changed := false
	out := make([]string, 0, len(aux))
	for _, id := range aux {
		if old, ok := legacyCursorModels[strings.TrimSpace(id)]; ok {
			id, changed = old.base, true
		}
		if id == primary || slices.Contains(out, id) {
			changed = true
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		out = nil
	}
	return out, changed
}

func migrateCursorRoutes(tx *gorm.DB, configurations []string, primary map[string]legacyCursorModel, table, set, configuration, model, effort, tier string) error {
	if !tx.Migrator().HasTable(table) {
		return nil
	}
	cols := []string{"id", configuration, model}
	for _, c := range []string{set, effort, tier} {
		if c != "" {
			cols = append(cols, c)
		}
	}
	for _, c := range cols {
		if !tx.Migrator().HasColumn(table, c) {
			return nil
		}
	}
	var rows []map[string]any
	if err := tx.Table(table).Select(cols).Where(configuration+" IN ?", configurations).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		id := strings.TrimSpace(columnText(row[model]))
		old, legacy := legacyCursorModels[id]
		if !legacy && id == "" && (set == "" || columnTrue(row[set])) {
			old, legacy = primary[columnText(row[configuration])]
			old.base = ""
		}
		if !legacy {
			continue
		}
		updates := map[string]any{}
		if id != "" {
			updates[model] = old.base
		}
		if effort != "" && old.effort != "" && strings.TrimSpace(columnText(row[effort])) == "" {
			updates[effort] = old.effort
		}
		if tier != "" && old.tier != "" && strings.TrimSpace(columnText(row[tier])) == "" {
			updates[tier] = old.tier
		}
		if len(updates) == 0 {
			continue
		}
		if err := tx.Table(table).Where("id = ?", row["id"]).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func columnText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

func columnTrue(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case int32:
		return v != 0
	case int:
		return v != 0
	case float64:
		return v != 0
	case []byte:
		return string(v) == "1" || string(v) == "true"
	case string:
		return v == "1" || v == "true"
	}
	return false
}
