package gitdetect

// RepoRoot returns the top of the git working tree containing the current
// directory, or "" outside a repository.
//
// Maestro writes each flow's path relative to whatever directory it ran in.
// Case ids are built from paths relative to this root instead, so running the
// same flows from a different directory does not give them new ids and split
// their history.
func RepoRoot() string {
	return run("rev-parse", "--show-toplevel")
}
