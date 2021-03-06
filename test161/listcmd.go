package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ops-class/test161"
	"github.com/parnurzeal/gorequest"
	"net/http"
	"os"
	"sort"
	"strings"
)

var listRemoteFlag bool

func doListCommand() int {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Missing argument to list command\n")
		return 1
	}

	switch os.Args[2] {
	case "targets":
		return doListTargets()
	case "tags":
		return doListTags()
	case "tests":
		return doListTests()
	case "all":
		return doListAll()
	default:
		fmt.Fprintf(os.Stderr, "Invalid option to 'test161 list'.  Must be one of (targets, tags, tests)\n")
		return 1
	}
}

func doListTargets() int {
	if err := getListArgs(); err != nil {
		printRunError(err)
		return 1
	}

	var targets *test161.TargetList

	if listRemoteFlag {
		var errs []error
		if targets, errs = getRemoteTargets(); len(errs) > 0 {
			printRunErrors(errs)
			return 1
		}
	} else {
		targets = env.TargetList()
	}

	printTargets(targets)

	return 0
}

func getRemoteTargets() (*test161.TargetList, []error) {
	if len(clientConf.Server) == 0 {
		return nil, []error{errors.New("server field missing in .test161.conf")}
	}

	endpoint := clientConf.Server + "/api-v1/targets"
	request := gorequest.New()

	resp, body, errs := request.Get(endpoint).End()
	if errs != nil {
		return nil, errs
	}

	if resp.StatusCode != http.StatusOK {
		return nil, []error{fmt.Errorf("Unable to retrieve remote targets: %v", resp.Status)}
	}

	targets := &test161.TargetList{}

	if err := json.Unmarshal([]byte(body), targets); err != nil {
		return nil, []error{err}
	}

	return targets, nil
}

func printTargets(list *test161.TargetList) {
	var desc string
	if listRemoteFlag {
		desc = "Remote Target"
	} else {
		desc = "Local Target"
	}

	headers := []*Heading{
		&Heading{
			Text:     desc,
			MinWidth: 20,
		},
		&Heading{
			Text: "Type",
		},
		&Heading{
			Text: "Version",
		},
		&Heading{
			Text:           "Points",
			RightJustified: true,
		},
	}

	data := make([][]string, 0)
	for _, t := range list.Targets {
		data = append(data, []string{
			t.Name, t.Type, fmt.Sprintf("v%v", t.Version), fmt.Sprintf("%v", t.Points),
		})
	}

	fmt.Println()
	printColumns(headers, data, defaultPrintConf)
	fmt.Println()
}

func getListArgs() error {

	listFlags := flag.NewFlagSet("test161 list-targets", flag.ExitOnError)
	listFlags.Usage = usage
	listFlags.BoolVar(&listRemoteFlag, "remote", false, "")
	listFlags.BoolVar(&listRemoteFlag, "r", false, "")

	listFlags.Parse(os.Args[3:]) // this may exit

	if len(listFlags.Args()) > 0 {
		return errors.New("test161 list-targets does not support positional arguments")
	}

	return nil
}

func getAllTests() ([]*test161.Test, []error) {
	conf := &test161.GroupConfig{
		Tests: []string{"**/*.t"},
		Env:   env,
	}

	tg, errs := test161.GroupFromConfig(conf)
	if len(errs) > 0 {
		return nil, errs
	}

	// Sort the tests by ID
	tests := make([]*test161.Test, 0)
	for _, t := range tg.Tests {
		tests = append(tests, t)
	}
	sort.Sort(testsByID(tests))

	return tests, nil
}

func doListTags() int {

	tags := make(map[string][]*test161.Test)

	// Load every test file
	tests, errs := getAllTests()
	if len(errs) > 0 {
		printRunErrors(errs)
		return 1
	}

	// Get a tagmap of tag name -> list of tests
	for _, test := range tests {
		for _, tag := range test.Tags {
			if _, ok := tags[tag]; !ok {
				tags[tag] = make([]*test161.Test, 0)
			}
			tags[tag] = append(tags[tag], test)
		}
	}

	sorted := make([]string, 0)
	for tag, _ := range tags {
		sorted = append(sorted, tag)
	}
	sort.Strings(sorted)

	// Printing
	fmt.Println()

	for _, tag := range sorted {
		fmt.Println(tag)
		for _, test := range tags[tag] {
			fmt.Println("    ", test.DependencyID)
		}
	}

	fmt.Println()

	return 0
}

func doListTests() int {

	headers := []*Heading{
		&Heading{
			Text: "Test ID",
		},
		&Heading{
			Text: "Name",
		},
		&Heading{
			Text: "Description",
		},
	}

	// Load every test file
	tests, errs := getAllTests()
	if len(errs) > 0 {
		printRunErrors(errs)
		return 1
	}

	// Print ID, line, description for each tests
	data := make([][]string, 0)
	for _, test := range tests {
		data = append(data, []string{
			test.DependencyID,
			test.Name,
			strings.TrimSpace(test.Description),
		})
	}

	fmt.Println()
	printColumns(headers, data, defaultPrintConf)
	fmt.Println()

	return 0
}

func doListAll() int {
	// Load every test file
	tests, errs := getAllTests()
	if len(errs) > 0 {
		printRunErrors(errs)
		return 1
	}

	tags := make(map[string]bool)

	for _, test := range tests {
		fmt.Println(test.DependencyID)
		for _, tag := range test.Tags {
			tags[tag] = true
		}
	}

	// Print tags
	for key, _ := range tags {
		fmt.Println(key)
	}

	// Print targets
	for _, target := range env.Targets {
		fmt.Println(target.Name)
	}
	return 0
}
