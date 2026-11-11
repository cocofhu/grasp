package kimicode

import "os"

func RenderConfig(baseURL, model string) string {
	return renderConfig(baseURL, model, "")
}

func WriteConfig(dir, baseURL, model string) error {
	return writeConfig(dir, baseURL, model, "")
}

func writeConfig(dir, baseURL, model, effort string) error {
	directory, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return writeConfigAt(directory, baseURL, model, effort)
}
