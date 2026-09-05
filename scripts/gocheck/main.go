// gocheck: the Go half of the contract checks, so the pre-implementation suite exercises the
// same code paths the production loader will.
//
//	gocheck digest <file.json|file.yaml>...   RFC 8785 canonical digest (path-aware normalisation,
//	                                          strict YAML: duplicate keys rejected, floats rejected)
//	gocheck mediatype <value>...              mime.ParseMediaType normalisation, quoted params and all
//	gocheck semver <value>...                 strict SemVer 2.0.0 validity
package main

import (
	"fmt"
	"mime"
	"os"
	"strings"
)

func normaliseMediaType(v string) (string, error) {
	mt, params, err := mime.ParseMediaType(v)
	if err != nil {
		return "", err
	}
	if v, ok := params["charset"]; ok {
		params["charset"] = strings.ToLower(v)
	}
	// FormatMediaType sorts attributes and re-quotes values that need it, so a parameter
	// containing a semicolon or a quote survives the round trip unambiguously.
	out := mime.FormatMediaType(strings.ToLower(mt), params)
	if out == "" {
		return "", fmt.Errorf("cannot format media type %q", v)
	}
	return out, nil
}

// validSemver implements SemVer 2.0.0 exactly: no leading zeroes in numeric identifiers,
// no empty identifiers, build metadata ignored for validity but still syntax-checked.
func validSemver(v string) bool {
	core, pre, build := v, "", ""
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core, build = core[:i], core[i+1:]
	}
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core, pre = core[:i], core[i+1:]
	}
	nums := strings.Split(core, ".")
	if len(nums) != 3 {
		return false
	}
	for _, n := range nums {
		if n == "" || (len(n) > 1 && n[0] == '0') {
			return false
		}
		for _, c := range n {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	if strings.Contains(v, "-") && pre == "" {
		return false
	}
	if pre != "" {
		for _, id := range strings.Split(pre, ".") {
			if id == "" {
				return false
			}
			numeric := true
			for _, c := range id {
				if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && c != '-' {
					return false
				}
				if c < '0' || c > '9' {
					numeric = false
				}
			}
			if numeric && len(id) > 1 && id[0] == '0' {
				return false
			}
		}
	}
	if strings.Contains(v, "+") && build == "" {
		return false
	}
	if build != "" {
		for _, id := range strings.Split(build, ".") {
			if id == "" {
				return false
			}
			for _, c := range id {
				if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && c != '-' {
					return false
				}
			}
		}
	}
	return true
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gocheck digest|mediatype|semver ...")
		os.Exit(2)
	}
	args := os.Args[2:]
	fail := 0
	switch os.Args[1] {
	case "digest":
		for _, f := range args {
			d, err := digestFile(f)
			if err != nil {
				fmt.Printf("ERROR  %s: %v\n", f, err)
				fail++
				continue
			}
			fmt.Printf("%s  %s\n", d, f)
		}
	case "mediatype":
		for _, v := range args {
			n, err := normaliseMediaType(v)
			if err != nil {
				fmt.Printf("ERROR  %s\n", v)
				continue
			}
			fmt.Printf("%s\n", n)
		}
	case "semver":
		for _, v := range args {
			fmt.Printf("%v\n", validSemver(v))
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand", os.Args[1])
		os.Exit(2)
	}
	if fail > 0 {
		os.Exit(1)
	}
}
