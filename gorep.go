package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"runtime"
)

type fileMode int32

const (
	FMODE_DIR fileMode = iota
	FMODE_FILE
	FMODE_LINE
	FMODE_INVALID
)

type report struct {
	complete bool
	fmode    fileMode
	fpath    string
	line     string
}

type gorep struct {
	bRecursive bool
	bFind      bool
	bGrep      bool
	pattern    *regexp.Regexp
}

func usage(progName string) {
	fmt.Printf("%s [-r] [-f] [-g] PATTERN PATH\n", path.Base(progName))
}

func main() {
	cpus := runtime.NumCPU()
	runtime.GOMAXPROCS(cpus)

	/* parse flag */
	requireRecursive := flag.Bool("r", true, "enable recursive search.")
	requireFile := flag.Bool("f", true, "enable file search.")
	requireGrep := flag.Bool("g", false, "enable grep.")
	flag.Parse()

	if flag.NArg() < 2 {
		usage(os.Args[0])
		os.Exit(0)
	}

	pattern := flag.Arg(0)
	fpath := flag.Arg(1)

	fmt.Printf("pattern:%s path:%s -r:%v -f:%v -g:%v\n", pattern, fpath,
		*requireRecursive, *requireFile, *requireGrep)

	/* create gorep */
	c := New(*requireRecursive, *requireFile, *requireGrep, pattern)

	/* make notify channel */
	chNotify := make(chan report)

	/* start gorep */
	go c.kick(fpath, chNotify)

	showReport(chNotify)
}

func showReport(chNotify <-chan report) {
	/* receive notify */
	for repo, ok := <-chNotify; ok; repo, ok = <-chNotify {
		switch repo.fmode {
		case FMODE_DIR:
			fmt.Printf("[Dir ] %s\n", repo.fpath)
		case FMODE_FILE:
			fmt.Printf("[File] %s\n", repo.fpath)
		case FMODE_LINE:
			fmt.Printf("[Grep] %s:%s\n", repo.fpath, repo.line)
		default:
			fmt.Fprintf(os.Stderr, "Illegal filemode (%d)\n", repo.fmode)
		}
	}
}

func New(requireRecursive, requireFile, requireGrep bool, pattern string) *gorep {
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	return &gorep{requireRecursive, requireFile, requireGrep, compiledPattern}
}

func (this gorep) kick(fpath string, chNotify chan<- report) {
	/* make child channel */
	chRelay := make(chan report, 10)
	nRoutines := 0

	nRoutines++
	go this.dive(fpath, chRelay)

	for nRoutines > 0 {
		relayRepo := <-chRelay

		if relayRepo.complete {
			nRoutines--
			continue
		}

		switch relayRepo.fmode {
		case FMODE_DIR:
			if this.bFind && this.pattern.MatchString(path.Base(relayRepo.fpath)) {
				chNotify <- relayRepo
			}
			if this.bRecursive {
				nRoutines++
				go this.dive(relayRepo.fpath, chRelay)
			}
		case FMODE_FILE:
			if this.bFind && this.pattern.MatchString(path.Base(relayRepo.fpath)) {
				chNotify <- relayRepo
			}
			if this.bGrep {
				nRoutines++
				go this.grep(relayRepo.fpath, chRelay)
			}
		case FMODE_LINE:
			chNotify <- relayRepo
		default:
			fmt.Fprintf(os.Stderr, "Illegal filemode (%d)\n", relayRepo.fmode)
		}
	}

	close(chRelay)
	close(chNotify)
}

func (this gorep) dive(dir string, chRelay chan<- report) {
	defer func() {
		chRelay <- report{true, FMODE_DIR, "", ""}
	}()

	/* expand dir */
	list, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	for _, finfo := range list {
		var fmode fileMode
		if finfo.IsDir() {
			fmode = FMODE_DIR
		} else {
			fmode = FMODE_FILE
		}
		chRelay <- report{false, fmode, dir + "/" + finfo.Name(), ""}
	}
}

func (this gorep) grep(fpath string, chRelay chan<- report) {
	defer func() {
		chRelay <- report{true, FMODE_LINE, "", ""}
	}()

	file, err := os.Open(fpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	defer file.Close()

	lineNumber := 0
	lineReader := bufio.NewReader(file)

	for {
		line, isPrefix, err := lineReader.ReadLine()
		if err != nil {
			return
		}
		lineNumber++
		fullLine := string(line)
		if isPrefix {
			fullLine = fullLine + "@@"
		}
		if this.pattern.MatchString(fullLine) {
			formatline := fmt.Sprintf("%d: %s", lineNumber, fullLine)
			chRelay <- report{false, FMODE_LINE, fpath, formatline}
		}
	}
}