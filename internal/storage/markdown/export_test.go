package markdown

// SetBeforeRename runs f just before each file is renamed into place.
func SetBeforeRename(r *Repo, f func(path string)) { r.beforeRename = f }
