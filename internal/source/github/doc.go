// Package github reads pull requests (found by branch, in every state),
// their timeline, reviews and check runs per commit SHA from the GitHub API
// and turns them into model events. It is read-only: it never writes to
// GitHub.
package github
