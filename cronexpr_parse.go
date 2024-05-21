/*!
 * Copyright 2013 Raymond Hill
 *
 * Modifications 2020 - HashiCorp
 *
 * Project: github.com/gorhill/cronexpr
 * File: cronexpr_parse.go
 * Version: 1.0
 * License: pick the one which suits you best:
 *   GPL v3 see <https://www.gnu.org/licenses/gpl.html>
 *   APL v2 see <http://www.apache.org/licenses/LICENSE-2.0>
 *
 */

package cronexpr

/******************************************************************************/

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

/******************************************************************************/
var (
	genericDefaultList = []int{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
		10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
		20, 21, 22, 23, 24, 25, 26, 27, 28, 29,
		30, 31, 32, 33, 34, 35, 36, 37, 38, 39,
		40, 41, 42, 43, 44, 45, 46, 47, 48, 49,
		50, 51, 52, 53, 54, 55, 56, 57, 58, 59,
	}
)

/******************************************************************************/

var (
	monthTokens = map[string]int{
		`1`: 1, `01`: 1, `jan`: 1, `january`: 1,
		`2`: 2, `02`: 2, `feb`: 2, `february`: 2,
		`3`: 3, `03`: 3, `mar`: 3, `march`: 3,
		`4`: 4, `04`: 4, `apr`: 4, `april`: 4,
		`5`: 5, `05`: 5, `may`: 5,
		`6`: 6, `06`: 6, `jun`: 6, `june`: 6,
		`7`: 7, `07`: 7, `jul`: 7, `july`: 7,
		`8`: 8, `08`: 8, `aug`: 8, `august`: 8,
		`9`: 9, `09`: 9, `sep`: 9, `september`: 9,
		`10`: 10, `oct`: 10, `october`: 10,
		`11`: 11, `nov`: 11, `november`: 11,
		`12`: 12, `dec`: 12, `december`: 12,
	}
	dowTokens = map[string]int{
		`0`: 0, `sun`: 0, `sunday`: 0,
		`1`: 1, `mon`: 1, `monday`: 1,
		`2`: 2, `tue`: 2, `tuesday`: 2,
		`3`: 3, `wed`: 3, `wednesday`: 3,
		`4`: 4, `thu`: 4, `thursday`: 4,
		`5`: 5, `fri`: 5, `friday`: 5,
		`6`: 6, `sat`: 6, `saturday`: 6,
		`7`: 0,
	}
)

/******************************************************************************/

func atoi(s string) int {
	num, err := strconv.Atoi(s)

	if err != nil {
		panic("Atoi function failed: " + err.Error())
	}
	return num
}

type fieldDescriptor struct {
	name         string
	min, max     int
	defaultList  []int
	valuePattern string
	atoi         func(string) int
}

var (
	secondDescriptor = fieldDescriptor{
		name:         "second",
		min:          0,
		max:          59,
		defaultList:  genericDefaultList[0:60],
		valuePattern: `0?[0-9]|[1-5][0-9]`,
		atoi:         atoi,
	}
	minuteDescriptor = fieldDescriptor{
		name:         "minute",
		min:          0,
		max:          59,
		defaultList:  genericDefaultList[0:60],
		valuePattern: `0?[0-9]|[1-5][0-9]`,
		atoi:         atoi,
	}
	hourDescriptor = fieldDescriptor{
		name:         "hour",
		min:          0,
		max:          23,
		defaultList:  genericDefaultList[0:24],
		valuePattern: `0?[0-9]|1[0-9]|2[0-3]`,
		atoi:         atoi,
	}
	domDescriptor = fieldDescriptor{
		name:         "day-of-month",
		min:          1,
		max:          31,
		defaultList:  genericDefaultList[1:32],
		valuePattern: `0?[1-9]|[12][0-9]|3[01]`,
		atoi:         atoi,
	}
	monthDescriptor = fieldDescriptor{
		name:         "month",
		min:          1,
		max:          12,
		defaultList:  genericDefaultList[1:13],
		valuePattern: `0?[1-9]|1[012]|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec|january|february|march|april|march|april|june|july|august|september|october|november|december`,
		atoi: func(s string) int {
			return monthTokens[s]
		},
	}
	dowDescriptor = fieldDescriptor{
		name:         "day-of-week",
		min:          0,
		max:          6,
		defaultList:  genericDefaultList[0:7],
		valuePattern: `0?[0-7]|sun|mon|tue|wed|thu|fri|sat|sunday|monday|tuesday|wednesday|thursday|friday|saturday`,
		atoi: func(s string) int {
			return dowTokens[s]
		},
	}
	yearDescriptor = fieldDescriptor{
		name:         "year",
		min:          1970,
		max:          3999,
		defaultList:  nil,
		valuePattern: `19[7-9][0-9]|2[0-9]{3}|3[0-9]{3}`, //`19[789][0-9]|20[0-9]{2}`,
		atoi:         atoi,
	}
)

/******************************************************************************/

var (
	layoutWildcard            = `^\*$|^\?$`
	layoutValue               = `^(%value%)$`
	layoutRange               = `^(%value%)-(%value%)$`
	layoutWildcardAndInterval = `^\*/(\d+)$`
	layoutValueAndInterval    = `^(%value%)/(\d+)$`
	layoutRangeAndInterval    = `^(%value%)-(%value%)/(\d+)$`
	layoutLastDom             = `^l$`
	layoutWorkdom             = `^(%value%)w$`
	layoutLastWorkdom         = `^lw$`
	layoutLastNthDom          = `l-(\d{1,2})$`
	layoutDowOfLastWeek       = `^(%value%)l$`
	layoutDowOfSpecificWeek   = `^(%value%)#([1-5])$`
	fieldFinder               = regexp.MustCompile(`\S+`)
	entryFinder               = regexp.MustCompile(`[^,]+`)
	layoutRegexp              = make(map[string]*regexp.Regexp)
	layoutRegexpLock          sync.Mutex
)

/******************************************************************************/

var cronNormalizer = strings.NewReplacer(
	"@yearly", "0 0 0 1 1 * *",
	"@annually", "0 0 0 1 1 * *",
	"@monthly", "0 0 0 1 * * *",
	"@weekly", "0 0 0 * * 0 *",
	"@daily", "0 0 0 * * * *",
	"@hourly", "0 0 * * * * *")

/******************************************************************************/

func (expr *Expression) secondFieldHandler(s string) error {
	var err error
	expr.secondList, err = genericFieldHandler(s, secondDescriptor)
	return err
}

/******************************************************************************/

func (expr *Expression) minuteFieldHandler(s string) error {
	var err error
	expr.minuteList, err = genericFieldHandler(s, minuteDescriptor)
	return err
}

/******************************************************************************/

func (expr *Expression) hourFieldHandler(s string) error {
	var err error
	expr.hourList, err = genericFieldHandler(s, hourDescriptor)
	return err
}

/******************************************************************************/

func (expr *Expression) monthFieldHandler(s string) error {
	var err error
	expr.monthList, err = genericFieldHandler(s, monthDescriptor)
	return err
}

/******************************************************************************/

func (expr *Expression) yearFieldHandler(s string) error {
	var err error
	expr.yearList, err = genericFieldHandler(s, yearDescriptor)
	return err
}

/******************************************************************************/

const (
	none = 0
	one  = 1
	span = 2
	all  = 3
)

type cronDirective struct {
	kind  int
	first int
	last  int
	step  int
	sbeg  int
	send  int
}

func genericFieldHandler(s string, desc fieldDescriptor) ([]int, error) {
	directives, err := genericFieldParse(s, desc)
	if err != nil {
		return nil, err
	}
	values := make(map[int]bool)
	for _, directive := range directives {
		switch directive.kind {
		case none:
			return nil, fmt.Errorf("syntax error in %s field: '%s'", desc.name, s[directive.sbeg:directive.send])
		case one:
			populateOne(values, directive.first)
		case span:
			populateMany(values, directive.first, directive.last, directive.step)
		case all:
			return desc.defaultList, nil
		}
	}
	return toList(values), nil
}

func (expr *Expression) dowFieldHandler(s string) error {
	expr.daysOfWeekRestricted = true
	expr.daysOfWeek = make(map[int]bool)
	expr.lastWeekDaysOfWeek = make(map[int]bool)
	expr.specificWeekDaysOfWeek = make(map[int]bool)

	directives, err := genericFieldParse(s, dowDescriptor)
	if err != nil {
		return err
	}

	for _, directive := range directives {
		switch directive.kind {
		case none:
			sdirective := s[directive.sbeg:directive.send]
			snormal := strings.ToLower(sdirective)
			// `5L`
			pairs := makeLayoutRegexp(layoutDowOfLastWeek, dowDescriptor.valuePattern).FindStringSubmatchIndex(snormal)
			if len(pairs) > 0 {
				populateOne(expr.lastWeekDaysOfWeek, dowDescriptor.atoi(snormal[pairs[2]:pairs[3]]))
			} else {
				// `5#3`
				pairs := makeLayoutRegexp(layoutDowOfSpecificWeek, dowDescriptor.valuePattern).FindStringSubmatchIndex(snormal)
				if len(pairs) > 0 {
					populateOne(expr.specificWeekDaysOfWeek, (dowDescriptor.atoi(snormal[pairs[4]:pairs[5]])-1)*7+(dowDescriptor.atoi(snormal[pairs[2]:pairs[3]])%7))
				} else {
					return fmt.Errorf("syntax error in day-of-week field: '%s'", sdirective)
				}
			}
		case one:
			populateOne(expr.daysOfWeek, directive.first)
		case span:
			// To properly handle spans that end in 7 (Sunday)
			if directive.last == 0 {
				directive.last = 6
			}
			populateMany(expr.daysOfWeek, directive.first, directive.last, directive.step)
		case all:
			populateMany(expr.daysOfWeek, directive.first, directive.last, directive.step)
			expr.daysOfWeekRestricted = false
		}
	}
	return nil
}

func (expr *Expression) domFieldHandler(s string) error {
	expr.daysOfMonthRestricted = true
	expr.lastDayOfMonth = false
	expr.lastWorkdayOfMonth = false
	expr.daysOfMonth = make(map[int]bool)     // days of month map
	expr.workdaysOfMonth = make(map[int]bool) // work days of month map
	expr.lastNthDayOfMonth = 0
	directives, err := genericFieldParse(s, domDescriptor)
	if err != nil {
		return err
	}

	for _, directive := range directives {
		switch directive.kind {
		case none:
			sdirective := s[directive.sbeg:directive.send]
			snormal := strings.ToLower(sdirective)
			// `L`
			if makeLayoutRegexp(layoutLastDom, domDescriptor.valuePattern).MatchString(snormal) {
				expr.lastDayOfMonth = true
			} else {
				// `LW`
				if makeLayoutRegexp(layoutLastWorkdom, domDescriptor.valuePattern).MatchString(snormal) {
					expr.lastWorkdayOfMonth = true
				} else {
					// `15W`
					pairs := makeLayoutRegexp(layoutWorkdom, domDescriptor.valuePattern).FindStringSubmatchIndex(snormal)
					if len(pairs) > 0 {
						populateOne(expr.workdaysOfMonth, domDescriptor.atoi(snormal[pairs[2]:pairs[3]]))
					} else {
						// `L-3`
						if makeLayoutRegexp(layoutLastNthDom, domDescriptor.valuePattern).MatchString(snormal) {
							expr.lastNthDayOfMonth = captureNumberFromExpression(snormal)
						} else {
							return fmt.Errorf("syntax error in day-of-month field: '%s'", sdirective)
						}
					}
				}
			}
		case one:
			populateOne(expr.daysOfMonth, directive.first)
		case span:
			populateMany(expr.daysOfMonth, directive.first, directive.last, directive.step)
		case all:
			populateMany(expr.daysOfMonth, directive.first, directive.last, directive.step)
			expr.daysOfMonthRestricted = false
		}
	}
	return nil
}

/******************************************************************************/

func populateOne(values map[int]bool, v int) {
	values[v] = true
}

func populateMany(values map[int]bool, min, max, step int) {
	for i := min; i <= max; i += step {
		values[i] = true
	}
}

func toList(set map[int]bool) []int {
	list := make([]int, len(set))
	i := 0
	for k := range set {
		list[i] = k
		i += 1
	}
	sort.Ints(list)
	return list
}

/******************************************************************************/

func genericFieldParse(s string, desc fieldDescriptor) ([]*cronDirective, error) {
	// At least one entry must be present
	indices := entryFinder.FindAllStringIndex(s, -1)
	if len(indices) == 0 {
		return nil, fmt.Errorf("%s field: missing directive", desc.name)
	}

	directives := make([]*cronDirective, 0, len(indices))

	for i := range indices {
		directive := cronDirective{
			sbeg: indices[i][0],
			send: indices[i][1],
		}
		snormal := strings.ToLower(s[indices[i][0]:indices[i][1]])
		// `*`
		if makeLayoutRegexp(layoutWildcard, desc.valuePattern).MatchString(snormal) {
			directive.kind = all
			directive.first = desc.min
			directive.last = desc.max
			directive.step = 1
			directives = append(directives, &directive)
			continue
		}
		// `5`
		if makeLayoutRegexp(layoutValue, desc.valuePattern).MatchString(snormal) {
			directive.kind = one
			directive.first = desc.atoi(snormal)
			directives = append(directives, &directive)
			continue
		}
		// `5-20`
		pairs := makeLayoutRegexp(layoutRange, desc.valuePattern).FindStringSubmatchIndex(snormal)
		if len(pairs) > 0 {
			directive.kind = span
			directive.first = desc.atoi(snormal[pairs[2]:pairs[3]])
			directive.last = desc.atoi(snormal[pairs[4]:pairs[5]])
			directive.step = 1
			directives = append(directives, &directive)
			continue
		}

		// `*/2`
		pairs = makeLayoutRegexp(layoutWildcardAndInterval, desc.valuePattern).FindStringSubmatchIndex(snormal)
		if len(pairs) > 0 {
			directive.kind = span
			directive.first = desc.min
			directive.last = desc.max
			directive.step = atoi(snormal[pairs[2]:pairs[3]])
			if directive.step < 1 || directive.step > desc.max {
				return nil, fmt.Errorf("invalid interval %s", snormal)
			}
			directives = append(directives, &directive)
			continue
		}
		// `5/2`
		pairs = makeLayoutRegexp(layoutValueAndInterval, desc.valuePattern).FindStringSubmatchIndex(snormal)
		if len(pairs) > 0 {
			directive.kind = span
			directive.first = desc.atoi(snormal[pairs[2]:pairs[3]])
			directive.last = desc.max
			directive.step = atoi(snormal[pairs[4]:pairs[5]])
			if directive.step < 1 || directive.step > desc.max {
				return nil, fmt.Errorf("invalid interval %s", snormal)
			}
			directives = append(directives, &directive)
			continue
		}
		// `5-20/2`
		pairs = makeLayoutRegexp(layoutRangeAndInterval, desc.valuePattern).FindStringSubmatchIndex(snormal)
		if len(pairs) > 0 {
			directive.kind = span
			directive.first = desc.atoi(snormal[pairs[2]:pairs[3]])
			directive.last = desc.atoi(snormal[pairs[4]:pairs[5]])
			directive.step = atoi(snormal[pairs[6]:pairs[7]])
			if directive.step < 1 || directive.step > desc.max {
				return nil, fmt.Errorf("invalid interval %s", snormal)
			}
			directives = append(directives, &directive)
			continue
		}
		// No behavior for this one, let caller deal with it
		directive.kind = none
		directives = append(directives, &directive)
	}
	return directives, nil
}

/******************************************************************************/

func makeLayoutRegexp(layout, value string) *regexp.Regexp {
	layoutRegexpLock.Lock()
	defer layoutRegexpLock.Unlock()

	layout = strings.Replace(layout, `%value%`, value, -1)
	re := layoutRegexp[layout]
	if re == nil {
		re = regexp.MustCompile(layout)
		layoutRegexp[layout] = re
	}
	return re
}

// This function is used to retrieve the number after the following expression L-number
// example: L-3 -> 3
// example: L-43 -> 43
// FYI : If the number is greater then the month days, the calculations are transfferred to previous month(s)
func captureNumberFromExpression(inputString string) int {
	re := regexp.MustCompile(layoutLastNthDom)
	submatchIndexes := re.FindStringSubmatchIndex(inputString)

	if len(submatchIndexes) > 0 {
		startIndex := submatchIndexes[2]
		endIndex := submatchIndexes[3]
		capturedNumberStr := inputString[startIndex:endIndex]

		capturedNumber, err := strconv.Atoi(capturedNumberStr)
		if err != nil {
			fmt.Println("Error in captureNumberFromExpression:", err)
			return 0
		}
		return capturedNumber
	} else {
		fmt.Println("No match found for desired layout in captureNumberFromExpression.")
	}
	return 0
}
