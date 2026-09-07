// Package projectsearcher finds repository checkouts in a GOPATH-style source
// tree and resolves short, human-typed names to exactly one of them.
//
// A source tree here is any directory holding checkouts three levels down, as
// {host}/{owner}/{repo}:
//
//	/Users/albttx/go/src/github.com/albttx/p
//	└──────── root ───────┘ └─host─┘ └owner┘ └repo┘
//
// The package does two things. [Scan] discovers the projects in such a tree,
// quickly and without descending into the checkouts themselves. [Match],
// [Filter], [Search] and [Resolve] then narrow that set down from whatever
// fragment a person actually typed — usually a bare repository name.
//
// A typical use resolves one project and does something with its path:
//
//	projects, err := projectsearcher.Scan("/Users/albttx/go/src")
//	if err != nil {
//		return err
//	}
//
//	p, err := projectsearcher.Resolve(projects, "gno")
//	switch {
//	case errors.Is(err, projectsearcher.ErrNotFound):
//		return fmt.Errorf("no such project")
//	case err != nil:
//		// An *AmbiguousError lists every candidate in its message.
//		return err
//	}
//	fmt.Println(p.Path()) // /Users/albttx/go/src/github.com/gnolang/gno
//
// Scanning is cheap enough to do on every invocation of a command-line tool:
// it costs one directory read per owner and one stat per candidate, and never
// reads anything inside a checkout. On a tree of 185 repositories containing
// 361 .git entries in total it takes a few milliseconds.
//
// Nothing in this package writes to disk or shells out, and [Scan] is the only
// function that touches the filesystem at all; the rest operate on the
// []Project it returns, so they are trivially testable and safe to call in a
// loop.
package projectsearcher
