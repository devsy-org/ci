package baseline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

const deletedPath = "/dev/null"

type diffState struct {
	file, gitPaths, newFile    string
	before, after              hunkRange
	pendingNew, fromNull, hunk bool
}

type hunkRange struct{ line, remaining int }

type addedLines map[int]bool

type fileChange struct {
	lines   addedLines
	created bool
}

func collectAddedLines(input io.Reader) (map[string]*fileChange, error) {
	var state diffState
	files := make(map[string]*fileChange)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(nil, math.MaxInt)
	for diffLine := 1; scanner.Scan(); diffLine++ {
		number, err := state.read(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("malformed diff at line %d: %w", diffLine, err)
		}
		if state.newFile != "" {
			fileChanges(files, state.newFile).created = true
		}
		switch {
		case number != 0:
			fileChanges(files, state.file).lines[number] = true
		case state.survives():
			fileChanges(files, state.file)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read diff: %w", err)
	}
	if !state.complete() {
		return nil, errors.New("malformed diff: truncated file header or hunk")
	}
	return files, nil
}

func (d diffState) survives() bool { return d.hunk && d.file != deletedPath }

func fileChanges(files map[string]*fileChange, name string) *fileChange {
	if files[name] == nil {
		files[name] = &fileChange{lines: make(addedLines)}
	}
	return files[name]
}

func (d diffState) complete() bool {
	return !d.pendingNew && (d.file == "" || d.hunk) && !d.inBody()
}

func (d diffState) inBody() bool {
	return d.before.remaining != 0 || d.after.remaining != 0
}

func (d *diffState) read(line string) (int, error) {
	d.newFile = ""
	if d.hunk && line == `\ No newline at end of file` {
		return 0, nil
	}
	if d.inBody() {
		return d.bodyLine(line)
	}
	return 0, d.header(line)
}

func (d *diffState) bodyLine(line string) (int, error) {
	number := 0
	switch {
	case strings.HasPrefix(line, "+"):
		d.after.remaining--
		number = d.after.line
		d.after.line++
	case strings.HasPrefix(line, "-"):
		d.before.remaining--
	case strings.HasPrefix(line, " "):
		d.before.remaining--
		d.after.remaining--
		d.after.line++
	default:
		return 0, errors.New("expected a hunk body line")
	}
	if d.before.remaining < 0 || d.after.remaining < 0 {
		return 0, errors.New("hunk body exceeds its header ranges")
	}
	return number, nil
}

func (d *diffState) header(line string) error {
	if d.pendingNew {
		file, err := diffDestination(line, "+++ ", "b/")
		d.file, d.pendingNew = file, false
		if d.fromNull {
			d.newFile = file
		}
		return err
	}
	if strings.HasPrefix(line, "@@ ") && d.file != "" {
		return d.startHunk(line)
	}
	if !d.complete() {
		return errors.New("expected a hunk header")
	}
	return d.fileHeader(line)
}

func (d *diffState) fileHeader(line string) error {
	paths, gitHeader := strings.CutPrefix(line, "diff --git ")
	switch {
	case gitHeader:
		*d = diffState{gitPaths: paths}
	case strings.HasPrefix(line, "--- "):
		d.pendingNew, d.hunk = true, false
		d.fromNull = line == "--- /dev/null"
	case d.gitPaths != "" && d.file == "":
		return d.gitMetadata(line)
	default:
		return errors.New("unexpected diff header")
	}
	return nil
}

func (d *diffState) gitMetadata(line string) error {
	switch {
	case strings.HasPrefix(line, "rename to "), strings.HasPrefix(line, "copy to "):
		prefix, _, _ := strings.Cut(line, " to ")
		file, err := diffDestination(line, prefix+" to ", "")
		d.newFile = file
		return err
	case strings.HasPrefix(line, "new file mode "):
		file, err := newGitPath(d.gitPaths)
		d.newFile = file
		return err
	case strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ"):
		*d = diffState{}
	case isGitMetadata(line):
	default:
		return errors.New("unexpected Git diff metadata")
	}
	return nil
}

func isGitMetadata(line string) bool {
	first, _, _ := strings.Cut(line, " ")
	return slices.Contains(
		[]string{"index", "old", "new", "deleted", "similarity", "dissimilarity", "rename", "copy"},
		first,
	)
}

func newGitPath(paths string) (string, error) {
	// For a creation Git writes equal-length a/ and b/ paths, even when quoted.
	middle := len(paths) / 2
	if len(paths)%2 == 0 || paths[middle] != ' ' {
		return "", errors.New("invalid new file paths")
	}
	return diffDestination(paths[middle+1:], "", "b/")
}

func diffDestination(line, header, pathPrefix string) (string, error) {
	path, ok := strings.CutPrefix(line, header)
	if !ok {
		return "", errors.New("expected destination file header")
	}
	path = strings.TrimSuffix(path, "\t")
	if strings.HasPrefix(path, `"`) {
		decoded, err := strconv.Unquote(path)
		if err != nil {
			return "", errors.New("invalid quoted destination path")
		}
		path = decoded
	}
	if path == deletedPath {
		return path, nil
	}
	path, ok = strings.CutPrefix(path, pathPrefix)
	if !ok || path == "" {
		return "", errors.New("invalid destination path")
	}
	return path, nil
}

func (d *diffState) startHunk(header string) error {
	fields := strings.Fields(header)
	if len(fields) < 4 || fields[3] != "@@" {
		return errors.New("invalid hunk header")
	}
	before, oldOK := parseHunkRange(fields[1], "-")
	after, newOK := parseHunkRange(fields[2], "+")
	if !oldOK || !newOK || !d.validRanges(before, after) {
		return errors.New("invalid hunk ranges")
	}
	d.before, d.after, d.hunk = before, after, true
	return nil
}

func (d diffState) validRanges(before, after hunkRange) bool {
	return (before.remaining != 0 || after.remaining != 0) &&
		(d.file != deletedPath || after.remaining == 0)
}

func parseHunkRange(field, prefix string) (hunkRange, bool) {
	text, ok := strings.CutPrefix(field, prefix)
	if !ok {
		return hunkRange{}, false
	}
	start, length, explicit := strings.Cut(text, ",")
	if !explicit {
		length = "1"
	}
	line, lineErr := strconv.ParseUint(start, 10, strconv.IntSize-1)
	count, countErr := strconv.ParseUint(length, 10, strconv.IntSize-1)
	if lineErr != nil || countErr != nil || (line == 0 && count != 0) || count > math.MaxInt-line {
		return hunkRange{}, false
	}
	return hunkRange{line: int(line), remaining: int(count)}, true
}
