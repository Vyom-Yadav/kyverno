package jmespath

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	trunc "github.com/aquilax/truncate"
	"github.com/blang/semver/v4"
	gojmespath "github.com/jmespath/go-jmespath"
	wildcard "github.com/kyverno/kyverno/pkg/utils/wildcard"
	regen "github.com/zach-klippenstein/goregen"
	"golang.org/x/crypto/cryptobyte"
	cryptobyte_asn1 "golang.org/x/crypto/cryptobyte/asn1"
	"sigs.k8s.io/yaml"
)

type PublicKey struct {
	N string
	E int
}

// function names
var (
	compare                = "compare"
	equalFold              = "equal_fold"
	replace                = "replace"
	replaceAll             = "replace_all"
	toUpper                = "to_upper"
	toLower                = "to_lower"
	trim                   = "trim"
	trimPrefix             = "trim_prefix"
	split                  = "split"
	regexReplaceAll        = "regex_replace_all"
	regexReplaceAllLiteral = "regex_replace_all_literal"
	regexMatch             = "regex_match"
	patternMatch           = "pattern_match"
	labelMatch             = "label_match"
	toBoolean              = "to_boolean"
	add                    = "add"
	sum                    = "sum"
	subtract               = "subtract"
	multiply               = "multiply"
	divide                 = "divide"
	modulo                 = "modulo"
	base64Decode           = "base64_decode"
	base64Encode           = "base64_encode"
	pathCanonicalize       = "path_canonicalize"
	truncate               = "truncate"
	semverCompare          = "semver_compare"
	parseJson              = "parse_json"
	parseYAML              = "parse_yaml"
	items                  = "items"
	objectFromLists        = "object_from_lists"
	random                 = "random"
	x509_decode            = "x509_decode"
)

func GetFunctions() []FunctionEntry {
	return []FunctionEntry{{
		FunctionEntry: gojmespath.FunctionEntry{
			Name: compare,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpfCompare,
		},
		ReturnType: []jpType{jpNumber},
		Note:       "compares two strings lexicographically",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: equalFold,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpfEqualFold,
		},
		ReturnType: []jpType{jpBool},
		Note:       "allows comparing two strings for equivalency where the only differences are letter cases",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: replace,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
				{Types: []jpType{jpNumber}},
			},
			Handler: jpfReplace,
		},
		ReturnType: []jpType{jpString},
		Note:       "replaces a specified number of instances of the source string with the replacement string in a parent ",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: replaceAll,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpfReplaceAll,
		},
		ReturnType: []jpType{jpString},
		Note:       "replace all instances of one string with another in an overall parent string",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: toUpper,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpfToUpper,
		},
		ReturnType: []jpType{jpString},
		Note:       "takes in a string and outputs the same string with all upper-case letters",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: toLower,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpfToLower,
		},
		ReturnType: []jpType{jpString},
		Note:       "takes in a string and outputs the same string with all lower-case letters",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: trim,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpfTrim,
		},
		ReturnType: []jpType{jpString},
		Note:       "trims both ends of the source string by characters appearing in the second string",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: trimPrefix,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpfTrimPrefix,
		},
		ReturnType: []jpType{jpString},
		Note:       "trims the second string prefix from the first string if the first string starts with the prefix",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: split,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpfSplit,
		},
		ReturnType: []jpType{jpArrayString},
		Note:       "splits the first string when the second string is found and converts it into an array ",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: regexReplaceAll,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString, jpNumber}},
				{Types: []jpType{jpString, jpNumber}},
			},
			Handler: jpRegexReplaceAll,
		},
		ReturnType: []jpType{jpString},
		Note:       "converts all parameters to string",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: regexReplaceAllLiteral,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString, jpNumber}},
				{Types: []jpType{jpString, jpNumber}},
			},
			Handler: jpRegexReplaceAllLiteral,
		},
		ReturnType: []jpType{jpString},
		Note:       "converts all parameters to string",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: regexMatch,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString, jpNumber}},
			},
			Handler: jpRegexMatch,
		},
		ReturnType: []jpType{jpBool},
		Note:       "first string is the regular exression which is compared with second input which can be a number or string",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: patternMatch,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString, jpNumber}},
			},
			Handler: jpPatternMatch,
		},
		ReturnType: []jpType{jpBool},
		Note:       "'*' matches zero or more alphanumeric characters, '?' matches a single alphanumeric character",
	}, {
		// Validates if label (param1) would match pod/host/etc labels (param2)
		FunctionEntry: gojmespath.FunctionEntry{
			Name: labelMatch,
			Arguments: []argSpec{
				{Types: []jpType{jpObject}},
				{Types: []jpType{jpObject}},
			},
			Handler: jpLabelMatch,
		},
		ReturnType: []jpType{jpBool},
		Note:       "object arguments must be enclosed in backticks; ex. `{{request.object.spec.template.metadata.labels}}`",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: toBoolean,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpToBoolean,
		},
		ReturnType: []jpType{jpBool},
		Note:       "It returns true or false for any string, such as 'True', 'TruE', 'False', 'FAlse', 'faLSE', etc.",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: add,
			Arguments: []argSpec{
				{Types: []jpType{jpAny}},
				{Types: []jpType{jpAny}},
			},
			Handler: jpAdd,
		},
		ReturnType: []jpType{jpAny},
		Note:       "does arithmetic addition of two specified values of numbers, quantities, and durations",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: sum,
			Arguments: []argSpec{
				{Types: []jpType{jpArray}},
			},
			Handler: jpSum,
		},
		ReturnType: []jpType{jpAny},
		Note:       "does arithmetic addition of specified array of values of numbers, quantities, and durations",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: subtract,
			Arguments: []argSpec{
				{Types: []jpType{jpAny}},
				{Types: []jpType{jpAny}},
			},
			Handler: jpSubtract,
		},
		ReturnType: []jpType{jpAny},
		Note:       "does arithmetic subtraction of two specified values of numbers, quantities, and durations",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: multiply,
			Arguments: []argSpec{
				{Types: []jpType{jpAny}},
				{Types: []jpType{jpAny}},
			},
			Handler: jpMultiply,
		},
		ReturnType: []jpType{jpAny},
		Note:       "does arithmetic multiplication of two specified values of numbers, quantities, and durations",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: divide,
			Arguments: []argSpec{
				{Types: []jpType{jpAny}},
				{Types: []jpType{jpAny}},
			},
			Handler: jpDivide,
		},
		ReturnType: []jpType{jpAny},
		Note:       "divisor must be non zero",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: modulo,
			Arguments: []argSpec{
				{Types: []jpType{jpAny}},
				{Types: []jpType{jpAny}},
			},
			Handler: jpModulo,
		},
		ReturnType: []jpType{jpAny},
		Note:       "divisor must be non-zero, arguments must be integers",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: base64Decode,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpBase64Decode,
		},
		ReturnType: []jpType{jpString},
		Note:       "decodes a base 64 string",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: base64Encode,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpBase64Encode,
		},
		ReturnType: []jpType{jpString},
		Note:       "encodes a regular, plaintext and unencoded string to base64",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeSince,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeSince,
		},
		ReturnType: []jpType{jpString},
		Note:       "calculate the difference between a start and end period of time where the end may either be a static definition or the then-current time",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name:    timeNow,
			Handler: jpTimeNow,
		},
		ReturnType: []jpType{jpString},
		Note:       "returns current time in RFC 3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name:    timeNowUtc,
			Handler: jpTimeNowUtc,
		},
		ReturnType: []jpType{jpString},
		Note:       "returns current UTC time in RFC 3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: pathCanonicalize,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpPathCanonicalize,
		},
		ReturnType: []jpType{jpString},
		Note:       "normalizes or canonicalizes a given path by removing excess slashes",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: truncate,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpNumber}},
			},
			Handler: jpTruncate,
		},
		ReturnType: []jpType{jpString},
		Note:       "length argument must be enclosed in backticks; ex. \"{{request.object.metadata.name | truncate(@, `9`)}}\"",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: semverCompare,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpSemverCompare,
		},
		ReturnType: []jpType{jpBool},
		Note:       "compares two strings which comply with the semantic versioning schema and outputs a boolean response as to the position of the second relative to the first",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: parseJson,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpParseJson,
		},
		ReturnType: []jpType{jpAny},
		Note:       "decodes a valid JSON encoded string to the appropriate type. Opposite of `to_string` function",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: parseYAML,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpParseYAML,
		},
		ReturnType: []jpType{jpAny},
		Note:       "decodes a valid YAML encoded string to the appropriate type provided it can be represented as JSON",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: items,
			Arguments: []argSpec{
				{Types: []jpType{jpObject, jpArray}},
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpItems,
		},
		ReturnType: []jpType{jpArray},
		Note:       "converts a map or array to an array of objects where each key:value is an item in the array",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: objectFromLists,
			Arguments: []argSpec{
				{Types: []jpType{jpArray}},
				{Types: []jpType{jpArray}},
			},
			Handler: jpObjectFromLists,
		},
		ReturnType: []jpType{jpObject},
		Note:       "converts a pair of lists containing keys and values to an object",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: random,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpRandom,
		},
		ReturnType: []jpType{jpString},
		Note:       "Generates a random sequence of characters",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: x509_decode,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpX509Decode,
		},
		ReturnType: []jpType{jpObject},
		Note:       "decodes an x.509 certificate to an object. you may also use this in conjunction with `base64_decode` jmespath function to decode a base64-encoded certificate",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeToCron,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeToCron,
		},
		ReturnType: []jpType{jpString},
		Note:       "converts a time (RFC 3339) to a cron expression (string).",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeAdd,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeAdd,
		},
		ReturnType: []jpType{jpString},
		Note:       "adds duration (second string) to a time value (first string)",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeParse,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeParse,
		},
		ReturnType: []jpType{jpString},
		Note:       "changes a time value of a given layout to RFC 3339",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeUtc,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeUtc,
		},
		ReturnType: []jpType{jpString},
		Note:       "calcutes time in UTC from a given time in RFC 3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeDiff,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeDiff,
		},
		ReturnType: []jpType{jpString},
		Note:       "calculate the difference between a start and end date in RFC3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeBefore,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeBefore,
		},
		ReturnType: []jpType{jpBool},
		Note:       "checks if a time is before another time, both in RFC3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeAfter,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeAfter,
		},
		ReturnType: []jpType{jpBool},
		Note:       "checks if a time is after another time, both in RFC3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeBetween,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeBetween,
		},
		ReturnType: []jpType{jpBool},
		Note:       "checks if a time is between a start and end time, all in RFC3339 format",
	}, {
		FunctionEntry: gojmespath.FunctionEntry{
			Name: timeTruncate,
			Arguments: []argSpec{
				{Types: []jpType{jpString}},
				{Types: []jpType{jpString}},
			},
			Handler: jpTimeTruncate,
		},
		ReturnType: []jpType{jpString},
		Note:       "returns the result of rounding time down to a multiple of duration",
	}}
}

func jpfCompare(arguments []interface{}) (interface{}, error) {
	if a, err := validateArg(compare, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else if b, err := validateArg(compare, arguments, 1, reflect.String); err != nil {
		return nil, err
	} else {
		return strings.Compare(a.String(), b.String()), nil
	}
}

func jpfEqualFold(arguments []interface{}) (interface{}, error) {
	if a, err := validateArg(equalFold, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else if b, err := validateArg(equalFold, arguments, 1, reflect.String); err != nil {
		return nil, err
	} else {
		return strings.EqualFold(a.String(), b.String()), nil
	}
}

func jpfReplace(arguments []interface{}) (interface{}, error) {
	if str, err := validateArg(replace, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else if old, err := validateArg(replace, arguments, 1, reflect.String); err != nil {
		return nil, err
	} else if new, err := validateArg(replace, arguments, 2, reflect.String); err != nil {
		return nil, err
	} else if n, err := validateArg(replace, arguments, 3, reflect.Float64); err != nil {
		return nil, err
	} else {
		return strings.Replace(str.String(), old.String(), new.String(), int(n.Float())), nil
	}
}

func jpfReplaceAll(arguments []interface{}) (interface{}, error) {
	if str, err := validateArg(replaceAll, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else if old, err := validateArg(replaceAll, arguments, 1, reflect.String); err != nil {
		return nil, err
	} else if new, err := validateArg(replaceAll, arguments, 2, reflect.String); err != nil {
		return nil, err
	} else {
		return strings.ReplaceAll(str.String(), old.String(), new.String()), nil
	}
}

func jpfToUpper(arguments []interface{}) (interface{}, error) {
	if str, err := validateArg(toUpper, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else {
		return strings.ToUpper(str.String()), nil
	}
}

func jpfToLower(arguments []interface{}) (interface{}, error) {
	if str, err := validateArg(toLower, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else {
		return strings.ToLower(str.String()), nil
	}
}

func jpfTrim(arguments []interface{}) (interface{}, error) {
	var err error
	str, err := validateArg(trim, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	cutset, err := validateArg(trim, arguments, 1, reflect.String)
	if err != nil {
		return nil, err
	}

	return strings.Trim(str.String(), cutset.String()), nil
}

func jpfTrimPrefix(arguments []interface{}) (interface{}, error) {
	var err error
	str, err := validateArg(trimPrefix, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	prefix, err := validateArg(trimPrefix, arguments, 1, reflect.String)
	if err != nil {
		return nil, err
	}

	return strings.TrimPrefix(str.String(), prefix.String()), nil
}

func jpfSplit(arguments []interface{}) (interface{}, error) {
	var err error
	str, err := validateArg(split, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	sep, err := validateArg(split, arguments, 1, reflect.String)
	if err != nil {
		return nil, err
	}

	split := strings.Split(str.String(), sep.String())
	arr := make([]interface{}, len(split))

	for i, v := range split {
		arr[i] = v
	}

	return arr, nil
}

func jpRegexReplaceAll(arguments []interface{}) (interface{}, error) {
	var err error
	regex, err := validateArg(regexReplaceAll, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	src, err := ifaceToString(arguments[1])
	if err != nil {
		return nil, formatError(invalidArgumentTypeError, regexReplaceAll, 2, "String or Real")
	}

	repl, err := ifaceToString(arguments[2])
	if err != nil {
		return nil, formatError(invalidArgumentTypeError, regexReplaceAll, 3, "String or Real")
	}

	reg, err := regexp.Compile(regex.String())
	if err != nil {
		return nil, formatError(genericError, regexReplaceAll, err.Error())
	}
	return string(reg.ReplaceAll([]byte(src), []byte(repl))), nil
}

func jpRegexReplaceAllLiteral(arguments []interface{}) (interface{}, error) {
	var err error
	regex, err := validateArg(regexReplaceAllLiteral, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	src, err := ifaceToString(arguments[1])
	if err != nil {
		return nil, formatError(invalidArgumentTypeError, regexReplaceAllLiteral, 2, "String or Real")
	}

	repl, err := ifaceToString(arguments[2])
	if err != nil {
		return nil, formatError(invalidArgumentTypeError, regexReplaceAllLiteral, 3, "String or Real")
	}

	reg, err := regexp.Compile(regex.String())
	if err != nil {
		return nil, formatError(genericError, regexReplaceAllLiteral, err.Error())
	}
	return string(reg.ReplaceAllLiteral([]byte(src), []byte(repl))), nil
}

func jpRegexMatch(arguments []interface{}) (interface{}, error) {
	var err error
	regex, err := validateArg(regexMatch, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	src, err := ifaceToString(arguments[1])
	if err != nil {
		return nil, formatError(invalidArgumentTypeError, regexMatch, 2, "String or Real")
	}

	return regexp.Match(regex.String(), []byte(src))
}

func jpPatternMatch(arguments []interface{}) (interface{}, error) {
	pattern, err := validateArg(regexMatch, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	src, err := ifaceToString(arguments[1])
	if err != nil {
		return nil, formatError(invalidArgumentTypeError, regexMatch, 2, "String or Real")
	}

	return wildcard.Match(pattern.String(), src), nil
}

func jpLabelMatch(arguments []interface{}) (interface{}, error) {
	labelMap, ok := arguments[0].(map[string]interface{})

	if !ok {
		return nil, formatError(invalidArgumentTypeError, labelMatch, 0, "Object")
	}

	matchMap, ok := arguments[1].(map[string]interface{})

	if !ok {
		return nil, formatError(invalidArgumentTypeError, labelMatch, 1, "Object")
	}

	for key, value := range labelMap {
		if val, ok := matchMap[key]; !ok || val != value {
			return false, nil
		}
	}

	return true, nil
}

func jpToBoolean(arguments []interface{}) (interface{}, error) {
	if input, err := validateArg(toBoolean, arguments, 0, reflect.String); err != nil {
		return nil, err
	} else {
		switch strings.ToLower(input.String()) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, formatError(genericError, toBoolean, fmt.Sprintf("lowercase argument must be 'true' or 'false' (provided: '%s')", input.String()))
		}
	}
}

func _jpAdd(arguments []interface{}, operator string) (interface{}, error) {
	op1, op2, err := ParseArithemticOperands(arguments, operator)
	if err != nil {
		return nil, err
	}
	return op1.Add(op2, operator)
}

func jpAdd(arguments []interface{}) (interface{}, error) {
	return _jpAdd(arguments, add)
}

func jpSum(arguments []interface{}) (interface{}, error) {
	items, ok := arguments[0].([]interface{})
	if !ok {
		return nil, formatError(typeMismatchError, sum)
	}
	if len(items) == 0 {
		return nil, formatError(genericError, sum, "at least one element in the array is required")
	}
	var err error
	result := items[0]
	for _, item := range items[1:] {
		result, err = _jpAdd([]interface{}{result, item}, sum)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func jpSubtract(arguments []interface{}) (interface{}, error) {
	op1, op2, err := ParseArithemticOperands(arguments, subtract)
	if err != nil {
		return nil, err
	}

	return op1.Subtract(op2)
}

func jpMultiply(arguments []interface{}) (interface{}, error) {
	op1, op2, err := ParseArithemticOperands(arguments, multiply)
	if err != nil {
		return nil, err
	}

	return op1.Multiply(op2)
}

func jpDivide(arguments []interface{}) (interface{}, error) {
	op1, op2, err := ParseArithemticOperands(arguments, divide)
	if err != nil {
		return nil, err
	}

	return op1.Divide(op2)
}

func jpModulo(arguments []interface{}) (interface{}, error) {
	op1, op2, err := ParseArithemticOperands(arguments, modulo)
	if err != nil {
		return nil, err
	}

	return op1.Modulo(op2)
}

func jpBase64Decode(arguments []interface{}) (interface{}, error) {
	var err error
	str, err := validateArg("", arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	decodedStr, err := base64.StdEncoding.DecodeString(str.String())
	if err != nil {
		return nil, err
	}

	return string(decodedStr), nil
}

func jpBase64Encode(arguments []interface{}) (interface{}, error) {
	var err error
	str, err := validateArg("", arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	return base64.StdEncoding.EncodeToString([]byte(str.String())), nil
}

func jpPathCanonicalize(arguments []interface{}) (interface{}, error) {
	var err error
	str, err := validateArg(pathCanonicalize, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	return filepath.Join(str.String()), nil
}

func jpTruncate(arguments []interface{}) (interface{}, error) {
	var err error
	var normalizedLength float64
	str, err := validateArg(truncate, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}
	length, err := validateArg(truncate, arguments, 1, reflect.Float64)
	if err != nil {
		return nil, err
	}

	if length.Float() < 0 {
		normalizedLength = float64(0)
	} else {
		normalizedLength = length.Float()
	}

	return trunc.Truncator(str.String(), int(normalizedLength), trunc.CutStrategy{}), nil
}

func jpSemverCompare(arguments []interface{}) (interface{}, error) {
	var err error
	v, err := validateArg(semverCompare, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}

	r, err := validateArg(semverCompare, arguments, 1, reflect.String)
	if err != nil {
		return nil, err
	}

	version, _ := semver.Parse(v.String())
	expectedRange, err := semver.ParseRange(r.String())
	if err != nil {
		return nil, err
	}

	if expectedRange(version) {
		return true, nil
	}
	return false, nil
}

func jpParseJson(arguments []interface{}) (interface{}, error) {
	input, err := validateArg(parseJson, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}
	var output interface{}
	err = json.Unmarshal([]byte(input.String()), &output)
	return output, err
}

func jpParseYAML(arguments []interface{}) (interface{}, error) {
	input, err := validateArg(parseYAML, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}
	jsonData, err := yaml.YAMLToJSON([]byte(input.String()))
	if err != nil {
		return nil, err
	}
	var output interface{}
	err = json.Unmarshal(jsonData, &output)
	return output, err
}

func jpItems(arguments []interface{}) (interface{}, error) {
	keyName, ok := arguments[1].(string)
	if !ok {
		return nil, formatError(invalidArgumentTypeError, items, arguments, 1, "String")
	}
	valName, ok := arguments[2].(string)
	if !ok {
		return nil, formatError(invalidArgumentTypeError, items, arguments, 2, "String")
	}
	switch input := arguments[0].(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(input))
		// Sort the keys so that the output is deterministic
		for key := range input {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		arrayOfObj := make([]map[string]interface{}, 0, len(input))
		for _, key := range keys {
			arrayOfObj = append(arrayOfObj, map[string]interface{}{
				keyName: key,
				valName: input[key],
			})
		}
		return arrayOfObj, nil
	case []interface{}:
		arrayOfObj := make([]map[string]interface{}, 0, len(input))
		for index, value := range input {
			arrayOfObj = append(arrayOfObj, map[string]interface{}{
				keyName: float64(index),
				valName: value,
			})
		}
		return arrayOfObj, nil
	default:
		return nil, formatError(invalidArgumentTypeError, items, arguments, 0, "Object or Array")
	}
}

func jpObjectFromLists(arguments []interface{}) (interface{}, error) {
	keys, ok := arguments[0].([]interface{})
	if !ok {
		return nil, formatError(invalidArgumentTypeError, objectFromLists, arguments, 0, "Array")
	}
	values, ok := arguments[1].([]interface{})
	if !ok {
		return nil, formatError(invalidArgumentTypeError, objectFromLists, arguments, 1, "Array")
	}

	output := map[string]interface{}{}

	for i, ikey := range keys {
		key, err := ifaceToString(ikey)
		if err != nil {
			return nil, formatError(invalidArgumentTypeError, objectFromLists, arguments, 0, "StringArray")
		}
		if i < len(values) {
			output[key] = values[i]
		} else {
			output[key] = nil
		}
	}

	return output, nil
}

// InterfaceToString casts an interface to a string type
func ifaceToString(iface interface{}) (string, error) {
	switch i := iface.(type) {
	case int:
		return strconv.Itoa(i), nil
	case float64:
		return strconv.FormatFloat(i, 'f', -1, 32), nil
	case float32:
		return strconv.FormatFloat(float64(i), 'f', -1, 32), nil
	case string:
		return i, nil
	case bool:
		return strconv.FormatBool(i), nil
	default:
		return "", errors.New("error, undefined type cast")
	}
}

func jpRandom(arguments []interface{}) (interface{}, error) {
	pattern := arguments[0].(string)
	if pattern == "" {
		return "", errors.New("no pattern provided")
	}

	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	ans, err := regen.Generate(pattern)
	if err != nil {
		return nil, err
	}
	return ans, nil
}

func jpX509Decode(arguments []interface{}) (interface{}, error) {
	res := make(map[string]interface{})
	input, err := validateArg(x509_decode, arguments, 0, reflect.String)
	if err != nil {
		return nil, err
	}
	p, _ := pem.Decode([]byte(input.String()))
	if p == nil {
		return res, errors.New("invalid certificate")
	}

	cert, err := x509.ParseCertificate(p.Bytes)
	if err != nil {
		return res, err
	}

	buf := new(bytes.Buffer)
	if fmt.Sprint(cert.PublicKeyAlgorithm) == "RSA" {
		spki := cryptobyte.String(cert.RawSubjectPublicKeyInfo)
		if !spki.ReadASN1(&spki, cryptobyte_asn1.SEQUENCE) {
			return res, errors.New("writing asn.1 element to 'spki' failed")
		}
		var pkAISeq cryptobyte.String
		if !spki.ReadASN1(&pkAISeq, cryptobyte_asn1.SEQUENCE) {
			return res, errors.New("writing asn.1 element to 'pkAISeq' failed")
		}
		var spk asn1.BitString
		if !spki.ReadASN1BitString(&spk) {
			return res, errors.New("writing asn.1 bit string to 'spk' failed")
		}
		kk, err := x509.ParsePKCS1PublicKey(spk.Bytes)
		if err != nil {
			return res, err
		}

		cert.PublicKey = PublicKey{
			N: kk.N.String(),
			E: kk.E,
		}

		enc := json.NewEncoder(buf)
		err = enc.Encode(cert)
		if err != nil {
			return res, err
		}
	}

	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		return res, err
	}

	return res, nil
}
