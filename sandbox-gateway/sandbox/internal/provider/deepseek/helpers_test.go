package deepseek

import "backend/internal/acp"

func modelValue(options []acp.ConfigOption, model string) (string, error) {
	return modelValueForRoute(options, model, officialProvider, true)
}
