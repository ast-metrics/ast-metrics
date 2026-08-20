package configuration

import (
	"path/filepath"
)

// Scope binds an analyzed source to the configuration that governs the files
// under it. A monorepository declares one source per project, and a project
// holding its own configuration file is judged by that file alone: the closest
// configuration wins whole, configurations never merge.
type Scope struct {
	// Path is the analyzed source, absolute and cleaned.
	Path string

	// Configuration governs every file under Path. It is the root
	// configuration itself when the source holds no configuration file.
	Configuration *Configuration

	// Root is the directory the exclude patterns of Configuration are written
	// against. It is Path when the source holds its own configuration file, so
	// that such a file reads the same whether the project is analyzed inside
	// the monorepository or on its own. It is empty when the source inherits
	// the root configuration, whose patterns stay relative to the working
	// directory.
	Root string
}

// HasOwnConfiguration reports whether the scope is governed by a configuration
// file of its own rather than by the root one.
func (s Scope) HasOwnConfiguration() bool {
	return s.Root != ""
}

// ResolveScopes binds every analyzed source to the configuration that governs
// it, and stores the result in Scopes. A source directory holding a
// configuration file is governed by it; any other source is governed by the
// root configuration.
//
// A scope configuration is read for what it says about its own files, and
// never for the settings that describe a run as a whole. See scopeOnly.
func (c *Configuration) ResolveScopes() error {
	scopes := make([]Scope, 0, len(c.SourcesToAnalyzePath))

	for _, source := range c.SourcesToAnalyzePath {
		path, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		path = filepath.Clean(path)

		// The root configuration is already loaded, with the command line
		// options applied on top of it. Reading it a second time as a scope
		// configuration would drop them.
		filename := findConfigurationFile(path)
		if filename == "" || filename == c.ConfigurationFilePath {
			scopes = append(scopes, Scope{Path: path, Configuration: c})
			continue
		}

		scoped, err := loadScopeConfiguration(filename)
		if err != nil {
			return err
		}
		scopes = append(scopes, Scope{Path: path, Configuration: scoped, Root: path})
	}

	c.Scopes = scopes

	return nil
}

// scopeOnly drops the settings that describe a run rather than a scope. Where
// to write the reports, what to compare against, which sources to analyze and
// which baseline to apply are decisions taken once for the whole run, so a
// project configuration keeps only what it says about its own files.
func (c *Configuration) scopeOnly() {
	c.SourcesToAnalyzePath = nil
	c.Reports = ConfigurationReport{}
	c.CompareWith = ""
	c.Watching = false
	if c.Requirements != nil {
		c.Requirements.Baseline = ""
	}
}
