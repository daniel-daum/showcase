package gitx

import (
	"context"
	"strconv"
	"strings"
)

const maxDiffLines = 5000 // per commit; the rest is counted but not kept

type FileDiff struct {
	Path, OldPath     string // OldPath is set for renames
	Status            string // "added", "deleted", "renamed" or ""
	Added, Deleted    int
	Binary, Truncated bool
	Lines             []DiffLine
}

type DiffLine struct {
	Kind     byte // ' ', '+', '-' or '@' for a hunk header
	Old, New int  // line numbers, 0 if none
	Text     string
}

// Diff returns the changes a commit made, against its first parent (or the
// empty tree for a root commit).
func (r Repo) Diff(ctx context.Context, hash string) ([]FileDiff, error) {
	out, err := r.run(ctx, "show", "--format=", "--patch", "-M", "--no-color", "--no-ext-diff",
		"--diff-merges=first-parent", "--end-of-options", hash)
	if err != nil {
		return nil, err
	}
	var files []FileDiff
	var f *FileDiff
	kept, oldN, newN := 0, 0, 0
	inHunk := false
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, FileDiff{})
			f, inHunk = &files[len(files)-1], false
			if s := line[len("diff --git "):]; len(s)%2 == 1 { // "a/P b/P"
				f.Path = s[len(s)/2+3:]
			}
		case f == nil:
		case inHunk && line != "" && strings.ContainsRune(" +-\\", rune(line[0])):
			d := DiffLine{Kind: line[0], Text: line[1:]}
			switch d.Kind {
			case '+':
				f.Added++
				d.New, newN = newN, newN+1
			case '-':
				f.Deleted++
				d.Old, oldN = oldN, oldN+1
			case ' ':
				d.Old, d.New, oldN, newN = oldN, newN, oldN+1, newN+1
			default:
				continue // "\ No newline at end of file"
			}
			if kept < maxDiffLines {
				f.Lines, kept = append(f.Lines, d), kept+1
			} else {
				f.Truncated = true
			}
		case strings.HasPrefix(line, "@@ "):
			inHunk = true
			oldN, newN = hunkStart(line, '-'), hunkStart(line, '+')
			if kept < maxDiffLines {
				f.Lines, kept = append(f.Lines, DiffLine{Kind: '@', Text: line}), kept+1
			}
		case inHunk:
		case strings.HasPrefix(line, "new file"):
			f.Status = "added"
		case strings.HasPrefix(line, "deleted file"):
			f.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			f.Status, f.OldPath = "renamed", line[len("rename from "):]
		case strings.HasPrefix(line, "rename to "):
			f.Path = line[len("rename to "):]
		case strings.HasPrefix(line, "+++ b/"):
			f.Path = line[len("+++ b/"):]
		case strings.HasPrefix(line, "--- a/") && f.Path == "":
			f.Path = line[len("--- a/"):]
		case strings.HasPrefix(line, "Binary files "):
			f.Binary = true
		}
	}
	return files, nil
}

// hunkStart reads the start line after sign in "@@ -1,2 +3,4 @@".
func hunkStart(h string, sign byte) int {
	i := strings.IndexByte(h, sign)
	if i < 0 {
		return 0
	}
	s := h[i+1:]
	if j := strings.IndexAny(s, ", "); j >= 0 {
		s = s[:j]
	}
	n, _ := strconv.Atoi(s)
	return n
}
