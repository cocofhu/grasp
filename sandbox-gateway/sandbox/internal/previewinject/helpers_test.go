package previewinject

import (
	"net/http"
	"net/url"
)

func InjectHTML(doc []byte, scriptURL string) []byte {
	return scanHTML(doc, "").rewrite(doc, ResolveScriptURL(scriptURL), previewCSP{})
}

func NewHandler(upstream *url.URL, scriptURL string) http.Handler {
	return NewHandlerWithEmbed(upstream, scriptURL, EmbedLookup{})
}

func NewHandlerWithEmbed(upstream *url.URL, scriptURL string, embed EmbedLookup) http.Handler {
	return NewSwitchableHandler(upstream, "", scriptURL, embed)
}
