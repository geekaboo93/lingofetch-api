package resources

import (
	"bytes"
	_ "embed"
	"fmt"
	"math/rand"
	"strings"
	"text/template"
	"time"

	"github.com/goccy/go-yaml"
)

//go:embed resources.yaml
var resourcesFileData []byte

//go:embed auth_success.html
var authSuccessHTML []byte

type Resources struct {
	Prompts  map[string]string   `yaml:"prompts"`
	Messages map[string][]string `yaml:"messages"`
}

var globalResources *Resources

func init() {
	var res Resources
	if err := yaml.Unmarshal(resourcesFileData, &res); err != nil {
		panic(fmt.Sprintf("failed to unmarshal resources.yaml: %v", err))
	}
	globalResources = &res
	rand.Seed(time.Now().UnixNano())
}

// GetPrompt returns a formatted prompt using the given data
func GetPrompt(name string, data interface{}) (string, error) {
	tmplStr, ok := globalResources.Prompts[name]
	if !ok {
		return "", fmt.Errorf("prompt %s not found", name)
	}

	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse prompt template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute prompt template: %v", err)
	}

	return buf.String(), nil
}

// GetRandomMessage returns a random message from the specified category
func GetRandomMessage(category string) string {
	msgs, ok := globalResources.Messages[category]
	if !ok || len(msgs) == 0 {
		return ""
	}
	return msgs[rand.Intn(len(msgs))]
}

// GetAuthSuccessHTML returns the HTML for the authentication success page with the provider name
func GetAuthSuccessHTML(provider string) string {
	if provider == "" {
		provider = "Notion"
	}
	return strings.ReplaceAll(string(authSuccessHTML), "[PROVIDER]", provider)
}
