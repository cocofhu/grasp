package modelconnection

// modelInput keeps native adapters consistent about declared image support.
// An explicit input list takes precedence over the legacy vision flag. With
// neither declaration, leave the CLI's own capability defaults unchanged.
func modelInput(model Model) ([]string, *bool) {
	if len(model.Input) > 0 {
		images := false
		for _, input := range model.Input {
			if input == "image" {
				images = true
				break
			}
		}
		return model.Input, &images
	}
	if model.Capabilities.Vision == nil {
		return nil, nil
	}
	input := []string{"text"}
	if *model.Capabilities.Vision {
		input = append(input, "image")
	}
	return input, model.Capabilities.Vision
}
