// Package schema embeds and compiles the canonical JSON Schemas and
// exposes a Validate function for rolodex JSON bytes.
package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed schemas/*.json
var schemaFS embed.FS

const rolodexSchemaID = "https://dex.local/schema/rolodex.json"

var compiled *jsonschema.Schema

func init() {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020

	err := fs.WalkDir(schemaFS, "schemas", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		b, err := schemaFS.ReadFile(path)
		if err != nil {
			return err
		}
		// Each schema file's $id is what the compiler resolves by.
		// We pass an arbitrary URL here just to register the resource,
		// matching the file's relative name to its $id is done via $id itself.
		name := strings.TrimPrefix(path, "schemas/")
		if err := c.AddResource("https://dex.local/schema/"+name, strings.NewReader(string(b))); err != nil {
			return fmt.Errorf("add %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("schema: walk embedded fs: %w", err))
	}

	s, err := c.Compile(rolodexSchemaID)
	if err != nil {
		panic(fmt.Errorf("schema: compile rolodex: %w", err))
	}
	compiled = s
}

// Validate checks that raw is a JSON object conforming to the rolodex
// schema. It returns a *jsonschema.ValidationError on failure (which has
// rich detail via .DetailedOutput()).
func Validate(parsed any) error {
	if compiled == nil {
		return fmt.Errorf("schema: not initialized")
	}
	return compiled.Validate(parsed)
}
