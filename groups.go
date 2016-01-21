package test161

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bmatcuk/doublestar"
	"github.com/ops-class/test161/graph"
	//"io/ioutil"
	//"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type CompletedTestHandler func(test *Test, res *TestResult)

// GroupConfig specifies how a group of tests should be created and run.
// A TestGroup will be created and run using
type GroupConfig struct {
	Name    string   `json:name`
	RootDir string   `json:rootdir`
	UseDeps bool     `json:"usedeps"`
	TestDir string   `json:"testdir"`
	Tags    []string `json:"tags"`
	Tests   []string `json:"-"`
}

// A group of tests to be run
type TestGroup struct {
	id        uint64
	Tests     []*Test
	Config    GroupConfig
	Callbacks []CompletedTestHandler
}

type jsonTestGroup struct {
	Id     uint64      `json:"id"`
	Config GroupConfig `json:"config"`
	Tests  []*Test     `json:"tests"`
}

var idLock = &sync.Mutex{}
var curID uint64 = 1

// Id retrieves the group id
func (t *TestGroup) Id() uint64 {
	return t.id
}

// Custom JSON marshaling to deal with our read-only id
func (tg *TestGroup) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonTestGroup{tg.Id(), tg.Config, tg.Tests})
}

// Increments the global counter and returns the previous value
func incrementId() (res uint64) {
	idLock.Lock()
	res = curID
	curID += 1
	if curID == 0 {
		curID = 1
	}
	idLock.Unlock()
	return
}

// EmptyGroup creates an empty TestGroup that can be used to add
// groups from strings.
func EmptyGroup() *TestGroup {
	tg := &TestGroup{}
	tg.id = incrementId()
	tg.Tests = make([]*Test, 0)
	return tg
}

// TagMap store a slice of Tests corresponding to each tag
type TagMap map[string][]*Test

// Test Map stores Tests indexed by id and maintains a map
// of tag -> tests for the test set.
type TestMap struct {
	TestDir string
	Tests   map[string]*Test
	Tags    TagMap
}

// Result type for loading a test from a file
type testLoadResult struct {
	Test *Test
	Err  error
}

func NewTestMap(testDir string) (*TestMap, []error) {
	abs, err := filepath.Abs(testDir)
	if err != nil {
		return nil, []error{err}
	}
	abs = path.Clean(abs)
	tm := &TestMap{abs, make(map[string]*Test), make(TagMap)}
	errs := tm.load()
	if len(errs) > 0 {
		return nil, errs
	}

	tm.buildTagMap()

	return tm, nil
}

// Helper function to get an id for a particular filename.
// We use the filename relative to the test directory.
func idFromFile(filename, testDir string) (string, error) {
	if temp, err := filepath.Abs(filename); err != nil {
		return "", err
	} else {
		return filepath.Rel(testDir, path.Clean(temp))
	}
}

func (tm *TestMap) buildTagMap() {
	tm.Tags = make(TagMap)

	for _, test := range tm.Tests {
		for _, tag := range test.Tags {
			if _, ok := tm.Tags[tag]; !ok {
				tm.Tags[tag] = make([]*Test, 0)
			}
			tm.Tags[tag] = append(tm.Tags[tag], test)
		}
	}
}

// Helper function that just gets all test in the config test directory.
// It returns a mapping of test name (file name) to test.
func (tm *TestMap) load() []error {
	errs := make([]error, 0)

	// Find all test files using globstar snytax
	// This is relative to our working directory
	files, err := doublestar.Glob(fmt.Sprintf("%v/**/*.t", tm.TestDir))
	if err != nil {
		errs = append(errs, err)
		return errs
	}

	resChan := make(chan testLoadResult)

	// Spawn a bunch of workers to load the tests
	for _, file := range files {
		go func(f string) {
			test, err := TestFromFile(f)
			if err == nil {
				test.DependencyID, err = idFromFile(f, tm.TestDir)
			}
			res := testLoadResult{test, err}
			resChan <- res
		}(file)
	}

	// Retrieve the results
	for i := 0; i < len(files); i++ {
		res := <-resChan
		if res.Err != nil {
			errs = append(errs, res.Err)
		} else {
			tm.Tests[res.Test.DependencyID] = res.Test
		}
	}

	return errs
}

func (tm *TestMap) TestsFromGlob(search, startDir string) ([]*Test, error) {
	var glob string

	if strings.HasPrefix(search, "/") {
		// Relative to the test directory
		glob = path.Join(tm.TestDir, search)
	} else {
		// Relative to the path of the current file
		glob = path.Join(startDir, search)
	}

	// Test directories need to be self-contained. Clean up the
	// path and make that's where we're looking.
	glob = path.Clean(glob)

	if !strings.HasPrefix(glob, tm.TestDir) {
		fmt.Println(glob, tm.TestDir)
		return nil,
			errors.New(fmt.Sprintf("Cannot specify tests outside of testing directory: %v",
				glob))
	}

	// Get the files
	files, err := doublestar.Glob(glob)
	if err != nil {
		return nil, err
	}

	// Finally, create a slice of tests corresponding to the search string
	tests := make([]*Test, 0)
	for _, file := range files {
		if id, err := idFromFile(file, tm.TestDir); err != nil {
			return nil, err
		} else {
			if test, ok := tm.Tests[id]; ok {
				tests = append(tests, test)
			} else {
				return nil,
					errors.New(fmt.Sprintf("Cannot find test: %v.  Is TestMap initialized?", id))
			}
		}
	}
	return tests, nil
}

// Expand the dependencies for a single test
func (t *Test) expandTestDeps(tests *TestMap, done chan error) {

	t.ExpandedDeps = make(map[string]*Test)

	for _, dep := range t.Depends {
		var deps []*Test = nil
		var ok bool = false
		var err error = nil

		if strings.HasSuffix(dep, ".t") {
			// it's a file/glob
			startDir := path.Dir(path.Join(tests.TestDir, t.DependencyID))
			if deps, err = tests.TestsFromGlob(dep, startDir); err != nil {
				done <- err
				return
			} else if len(deps) == 0 {
				// The dependency doesn't exist at all
				done <- errors.New(fmt.Sprintf("No matches for dependency %v in test %v",
					dep, t.DependencyID))
				return
			}
		} else {
			// it's a tag, look it up in the tag map
			if deps, ok = tests.Tags[dep]; !ok {
				done <- errors.New(fmt.Sprintf("No matches for tag dependency '%v' in test '%v'",
					dep, t.DependencyID))
				return
			}
		}

		if deps != nil {
			for _, d := range deps {
				t.ExpandedDeps[d.DependencyID] = d
			}
		}
	}

	done <- nil
}

// Expand all tests' dependencies so we can create a dependency graph
func (tm *TestMap) expandAllDeps() []error {
	resChan := make(chan error)
	errors := make([]error, 0)

	// Expand all test dependencies in parallel
	for _, t := range tm.Tests {
		go t.expandTestDeps(tm, resChan)
	}

	// Read the results
	for i, count := 0, len(tm.Tests); i < count; i++ {
		res := <-resChan
		if res != nil {
			errors = append(errors, res)
		}
	}

	return errors
}

// Keyer interface for the dependency graph
func (t *Test) Key() string {
	return t.DependencyID
}

// DependencyGraph creates a dependency graph from the
// tests in TestMap
func (tm *TestMap) DependencyGraph() (*graph.Graph, []error) {
	errs := tm.expandAllDeps()
	if len(errs) > 0 {
		return nil, errs
	}

	// Nodes
	nodes := make([]graph.Keyer, 0, len(tm.Tests))
	for _, t := range tm.Tests {
		nodes = append(nodes, t)
	}

	// Our graph
	g := graph.New(nodes)

	// Edges.  There is an edge from A->B if A depends on B.

	errs = make([]error, 0)
	for _, test := range tm.Tests {
		for _, dep := range test.ExpandedDeps {
			err := g.AddEdge(test, dep)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	return g, errs
}

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// TODO
//  - single tests -> config.Tests
//  - dependency graph
//  - clean dependencies