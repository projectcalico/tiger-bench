package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"gopkg.in/yaml.v2"
)

type ResultsFile []map[string]interface{}
type TestConfigs []map[string]interface{}

func main() {
	refPath := "results.json.reference"
	genPath := "results.json"
	testsPath := "e2e-testconfig.yaml"

	ref, err := readResults(refPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Reference file error: %v\n", err)
		os.Exit(1)
	}

	gen, err := readResults(genPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Generated file error: %v\n", err)
		os.Exit(1)
	}

	tests, err := readTests(testsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Tests file error: %v\n", err)
		os.Exit(1)
	}
	numTests := len(tests)
	println("Number of tests in e2e-testconfig.yaml:", numTests)
	if len(gen) != numTests {
		fmt.Fprintf(os.Stderr, "results.json should contain %d test results, found %d\n", numTests, len(gen))
		os.Exit(1)
	}

	if !similarStructure(ref, gen) {
		fmt.Fprintf(os.Stderr, "results.json structure does not match reference\n")
		os.Exit(1)
	}

	fmt.Println("results.json is valid and matches reference structure.")
}

func readResults(path string) (ResultsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results ResultsFile
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func readTests(path string) (TestConfigs, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tests TestConfigs
	if err := yaml.Unmarshal(data, &tests); err != nil {
		return nil, err
	}
	return tests, nil
}

func similarStructure(a, b ResultsFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !compareTypes(a[i], b[i]) {
			return false
		}
	}
	return true
}

func compareTypes(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if !sameType(va, vb) {
			return false
		}
	}
	return true
}

func sameType(a, b interface{}) bool {
	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta == nil || tb == nil {
		return ta == tb
	}
	if ta.Kind() != tb.Kind() {
		return false
	}
	switch ta.Kind() {
	case reflect.Map:
		ma, ok1 := a.(map[string]interface{})
		mb, ok2 := b.(map[string]interface{})
		if !ok1 || !ok2 {
			return false
		}
		return compareTypes(ma, mb)
	case reflect.Slice:
		sa, ok1 := a.([]interface{})
		sb, ok2 := b.([]interface{})
		if !ok1 || !ok2 {
			return false
		}
		// Compare first element type if slices are non-empty
		if len(sa) > 0 && len(sb) > 0 {
			return sameType(sa[0], sb[0])
		}
		return len(sa) == len(sb)
	default:
		return ta.Kind() == tb.Kind()
	}
}
