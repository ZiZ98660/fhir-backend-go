package experiments

import "fmt"

// 1. Define a custom type based on an integer.
// This is the "Go way" to create an Enum.
type Gender int

// 2. Use iota to assign incrementing numbers automatically.
// iota starts at 0 and increments by 1 for each constant.
const (
	Unknown Gender = iota // 0
	Male                  // 1
	Female                // 2
	Other                 // 3
)

// 3. To make the Enum "human readable", we implement the Stringer interface.
// This allows us to call fmt.Println(Male) and see "Male" instead of "1".
func (g Gender) String() string {
	return [...]string{"Unknown", "Male", "Female", "Other"}[g]
}

// RunEnumsDemo is a helper function you can call from a test or main to see it work.
func RunEnumsDemo() {
	myGender := Male
	fmt.Printf("The value is %d, but the string is %s\n", myGender, myGender)
}

func Generics[T comparable](t T, s []T) int {
	for _, v := range s {
		if v == t {
			return 1
		}
	}
	return -1
}

var path = `repo->service->handler->main`
