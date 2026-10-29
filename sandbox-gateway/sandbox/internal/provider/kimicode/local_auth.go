package kimicode

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/pelletier/go-toml/v2"
)

// Kimi Code 2.4.0 resolves provider credentials from config.toml and file OAuth
// references from KIMI_CODE_HOME/credentials/<name>.json. The server has already
// copied the selected host login into root; retain only that account's auth in
// the final private home, alongside Grasp's model/site/effort configuration.
func writeLocalConfigAt(root string, home *os.Root, baseURL, model, effort string) error {
	source, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("Kimi Code 本机配置不可用")
	}
	defer func() { _ = source.Close() }()
	data, err := source.ReadFile("config.toml")
	var saved struct {
		DefaultModel string                    `toml:"default_model"`
		Providers    map[string]map[string]any `toml:"providers"`
		Models       map[string]struct {
			Provider string `toml:"provider"`
		} `toml:"models"`
	}
	if err != nil || toml.Unmarshal(data, &saved) != nil {
		return fmt.Errorf("Kimi Code 本机登录不可用，请先在 CLI 登录所选服务站点")
	}
	names := make([]string, 0, len(saved.Providers))
	for name := range saved.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	names = append([]string{saved.Models[saved.DefaultModel].Provider, "managed:kimi-code", "kimi"}, names...)
	var selected map[string]any
	for _, name := range names {
		provider := saved.Providers[name]
		if provider["type"] == "kimi" && strings.TrimRight(localString(provider["base_url"]), "/") == strings.TrimRight(baseURL, "/") {
			selected = provider
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("Kimi Code 本机登录不属于所选服务站点，请先在 CLI 登录该站点")
	}
	if localString(selected["api_key_env"]) != "" {
		return fmt.Errorf("Kimi Code 本机配置依赖环境变量，请先在 CLI 保存登录或使用工作区凭据")
	}
	key := localString(selected["api_key"])
	oauth, hasOAuth := selected["oauth"].(map[string]any)
	if key != "" && hasOAuth {
		return fmt.Errorf("Kimi Code 本机配置同时指定 API Key 与 OAuth，请在 CLI 修复登录")
	}
	var auth string
	switch {
	case hasOAuth:
		if err := copyLocalOAuth(source, home, oauth); err != nil {
			return err
		}
		auth = "[providers.kimi.oauth]\nstorage = \"file\"\nkey = " + tomlString(localString(oauth["key"])) + "\n"
		if host := localString(oauth["oauth_host"]); host != "" {
			auth += "oauth_host = " + tomlString(host) + "\n"
		}
	case key != "":
		auth = "api_key = " + tomlString(key) + "\n"
	default:
		env, _ := selected["env"].(map[string]any)
		if localString(env["KIMI_API_KEY"]) == "" {
			return fmt.Errorf("Kimi Code 本机登录不可用，请先在 CLI 登录所选服务站点")
		}
		auth = "api_key = " + tomlString(localString(env["KIMI_API_KEY"])) + "\n"
	}
	body := strings.Replace(renderConfig(baseURL, model, effort), "api_key_env = "+tomlString(APIKeyEnv)+"\n", auth, 1)
	return writeConfigBodyAt(home, body)
}

func localString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func copyLocalOAuth(source, home *os.Root, oauth map[string]any) error {
	// Match the native resolveKimiTokenStorageName/FileTokenStorage.pathFor
	// contract; never interpret the credential reference as a filesystem path.
	name := localString(oauth["key"])
	if name == "oauth/kimi-code" {
		name = "kimi-code"
	} else {
		name = strings.TrimPrefix(name, "oauth/")
	}
	if localString(oauth["storage"]) != "file" || name == "" || strings.HasPrefix(name, ".") || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("Kimi Code 本机 OAuth 文件引用无效")
	}
	credentials, err := source.OpenRoot("credentials")
	if err != nil {
		return fmt.Errorf("Kimi Code 本机 OAuth 登录文件不可用")
	}
	defer func() { _ = credentials.Close() }()
	input, err := credentials.OpenFile(name+".json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("Kimi Code 本机 OAuth 登录文件不可用")
	}
	defer func() { _ = input.Close() }()
	if info, err := input.Stat(); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Kimi Code 本机 OAuth 登录文件不可用")
	}
	data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return fmt.Errorf("Kimi Code 本机 OAuth 登录文件不可用")
	}
	if err := home.Mkdir("credentials", 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	if info, err := home.Lstat("credentials"); err != nil || !info.IsDir() {
		return fmt.Errorf("Kimi Code 私有 OAuth 目录不可用")
	}
	destination, err := home.OpenRoot("credentials")
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close() }()
	temporary := ".oauth-" + rand.Text()
	output, err := destination.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = destination.Remove(temporary) }()
	_, writeErr := output.Write(data)
	closeErr := output.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("Kimi Code 私有 OAuth 登录文件写入失败")
	}
	return destination.Rename(temporary, name+".json")
}
