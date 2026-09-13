package store

import (
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// environmentPalette is the fixed set of preset colors an environment may use.
// The frontend maps these keys to Tailwind classes, so an unknown key would
// render an unstyled badge: the server validates membership.
var environmentPalette = []string{"slate", "blue", "green", "amber", "orange", "red", "violet", "pink"}

// applyEnvironmentCreate appends a new environment to the setting. It rejects a
// duplicate title and derives a collision-free id from it.
func applyEnvironmentCreate(setting *storepb.EnvironmentSetting, title, color string) (*storepb.EnvironmentSetting_Environment, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, common.Errorf(common.Invalid, "environment title must not be empty")
	}
	if findEnvironmentByTitle(setting, title) >= 0 {
		return nil, common.Errorf(common.Conflict, "environment %q already exists", title)
	}
	id := uniqueEnvironmentID(setting, title)
	resolved, err := resolveEnvironmentColor(id, color)
	if err != nil {
		return nil, err
	}
	environment := &storepb.EnvironmentSetting_Environment{
		Id:    id,
		Title: title,
		Color: resolved,
	}
	setting.Environments = append(setting.Environments, environment)
	return environment, nil
}

// applyEnvironmentUpdate rewrites the mutable fields of one environment. The id
// and the stored tags stay untouched unless the patch names them.
func applyEnvironmentUpdate(setting *storepb.EnvironmentSetting, id string, patch *EnvironmentPatch) (*storepb.EnvironmentSetting_Environment, error) {
	index := findEnvironmentByID(setting, id)
	if index < 0 {
		return nil, common.Errorf(common.NotFound, "environment %q not found", id)
	}
	environment := setting.Environments[index]
	if title := patch.Title; title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed == "" {
			return nil, common.Errorf(common.Invalid, "environment title must not be empty")
		}
		if other := findEnvironmentByTitle(setting, trimmed); other >= 0 && other != index {
			return nil, common.Errorf(common.Conflict, "environment %q already exists", trimmed)
		}
		environment.Title = trimmed
	}
	if color := patch.Color; color != nil {
		resolved, err := resolveEnvironmentColor(id, *color)
		if err != nil {
			return nil, err
		}
		environment.Color = resolved
	}
	if patch.Tags != nil {
		environment.Tags = maps.Clone(patch.Tags)
	}
	return environment, nil
}

func applyEnvironmentDelete(setting *storepb.EnvironmentSetting, id string) error {
	index := findEnvironmentByID(setting, id)
	if index < 0 {
		return common.Errorf(common.NotFound, "environment %q not found", id)
	}
	setting.Environments = slices.Delete(setting.Environments, index, index+1)
	return nil
}

func findEnvironmentByID(setting *storepb.EnvironmentSetting, id string) int {
	for i, environment := range setting.GetEnvironments() {
		if environment.GetId() == id {
			return i
		}
	}
	return -1
}

func findEnvironmentByTitle(setting *storepb.EnvironmentSetting, title string) int {
	for i, environment := range setting.GetEnvironments() {
		if strings.EqualFold(environment.GetTitle(), title) {
			return i
		}
	}
	return -1
}

// uniqueEnvironmentID derives an id from the title and appends a counter until
// it no longer collides, so two titles that slug to the same value both get an
// id. A title with no ASCII letters or digits (for example an all-CJK name)
// falls back to "env".
func uniqueEnvironmentID(setting *storepb.EnvironmentSetting, title string) string {
	base := slugifyEnvironmentID(title)
	if base == "" {
		base = "env"
	}
	id := base
	for n := 1; findEnvironmentByID(setting, id) >= 0; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// slugifyEnvironmentID lowercases the title, collapses every run of characters
// outside [a-z0-9] into a single dash and trims the result. An empty result
// means the title carries no usable ASCII, and the caller falls back.
func slugifyEnvironmentID(title string) string {
	var builder strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingDash && builder.Len() > 0 {
				_ = builder.WriteByte('-')
			}
			pendingDash = false
			_, _ = builder.WriteRune(r)
			continue
		}
		pendingDash = true
	}
	return builder.String()
}

// resolveEnvironmentColor validates a caller-supplied palette key, or picks a
// stable default from the id when the caller left it empty.
func resolveEnvironmentColor(id, color string) (string, error) {
	color = strings.ToLower(strings.TrimSpace(color))
	if color == "" {
		return defaultEnvironmentColor(id), nil
	}
	if !slices.Contains(environmentPalette, color) {
		return "", common.Errorf(common.Invalid, "invalid environment color %q", color)
	}
	return color, nil
}

// defaultEnvironmentColor hashes the id into the palette so an environment
// keeps the same color across a delete/recreate.
func defaultEnvironmentColor(id string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	return environmentPalette[int(hash.Sum32())%len(environmentPalette)]
}
