package settings

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

// CombinedDomain is an admin-facing projection. Each child domain remains the
// authority for validating, storing, and applying its own settings.
type CombinedDomain struct {
	domain   string
	title    string
	children []SettingsDomain
	owners   map[string]SettingsDomain
}

func Combine(domain, title string, children ...SettingsDomain) (*CombinedDomain, error) {
	combined := &CombinedDomain{
		domain:   strings.TrimSpace(domain),
		title:    strings.TrimSpace(title),
		children: children,
		owners:   make(map[string]SettingsDomain),
	}
	for _, child := range children {
		if child == nil {
			continue
		}
		for _, field := range child.Fields() {
			if previous := combined.owners[field.Key]; previous != nil {
				return nil, fmt.Errorf("settings field %q belongs to both %s and %s", field.Key, previous.Domain(), child.Domain())
			}
			combined.owners[field.Key] = child
		}
	}
	return combined, nil
}

func (d *CombinedDomain) Domain() string { return d.domain }
func (d *CombinedDomain) Title() string  { return d.title }
func (d *CombinedDomain) Fields() []settingscore.Descriptor {
	fields := make([]settingscore.Descriptor, 0, len(d.owners))
	for _, child := range d.children {
		if child != nil {
			fields = append(fields, child.Fields()...)
		}
	}
	return fields
}
func (d *CombinedDomain) Get(ctx context.Context) (settingscore.Document, error) {
	return d.read(ctx, false)
}
func (d *CombinedDomain) Reload(ctx context.Context) (settingscore.Document, error) {
	return d.read(ctx, true)
}
func (d *CombinedDomain) Patch(ctx context.Context, values map[string]any) (settingscore.Document, []string, error) {
	grouped := make(map[SettingsDomain]map[string]any)
	for key, value := range values {
		owner := d.owners[key]
		if owner == nil {
			return settingscore.Document{}, nil, fmt.Errorf("unknown setting %q", key)
		}
		if grouped[owner] == nil {
			grouped[owner] = make(map[string]any)
		}
		grouped[owner][key] = value
	}
	restart := make([]string, 0)
	for _, child := range d.children {
		childValues := grouped[child]
		if len(childValues) == 0 {
			continue
		}
		_, childRestart, err := child.Patch(ctx, childValues)
		if err != nil {
			return settingscore.Document{}, nil, err
		}
		restart = append(restart, childRestart...)
	}
	document, err := d.Get(ctx)
	sort.Strings(restart)
	return document, restart, err
}

func (d *CombinedDomain) read(ctx context.Context, reload bool) (settingscore.Document, error) {
	document := settingscore.Document{Domain: d.domain, Title: d.title, Values: map[string]any{}, Sources: map[string]settingscore.Source{}}
	refreshErrors := make([]string, 0)
	for _, child := range d.children {
		if child == nil {
			continue
		}
		var childDocument settingscore.Document
		var err error
		if reload {
			childDocument, err = child.Reload(ctx)
		} else {
			childDocument, err = child.Get(ctx)
		}
		if err != nil {
			return settingscore.Document{}, err
		}
		for key, value := range childDocument.Values {
			document.Values[key] = value
		}
		for key, source := range childDocument.Sources {
			document.Sources[key] = source
		}
		document.Fields = append(document.Fields, childDocument.Fields...)
		if childDocument.UpdatedAt.After(document.UpdatedAt) {
			document.UpdatedAt = childDocument.UpdatedAt
		}
		if childDocument.Stale {
			document.Stale = true
			refreshErrors = append(refreshErrors, childDocument.RefreshError)
		}
	}
	if document.UpdatedAt.IsZero() {
		document.UpdatedAt = time.Time{}
	}
	document.RefreshError = strings.Join(refreshErrors, "; ")
	return document, nil
}
