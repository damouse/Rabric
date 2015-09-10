package rabric

import (
    "fmt"
    "strings"
    "regexp"
)

func validEndpoint(s string) bool {
    r, _ := regexp.Compile("(^([a-z]+)(.[a-z]+)*$)")
    return r.MatchString(s)
}

// Regex valid domains
// (([a-z]+)(.[a-z]+)*)

// Given a uri, attempts to extract the target action naively
// Please move me somewhere nice
func extractActions(s string) (string, error) {
    i := strings.Index(s, "/")

    // No slash found, error
    if i == -1 {
        return "", InvalidURIError(s)
    }

    // not covered: closing slash
    // pd.damouse/

    i += 1
    return s[i:], nil
}


func testRexex(s string) {
    r, _ := regexp.Compile("(([a-z]+)(.[a-z]+)*)")
    fmt.Printf("Regext for %s matches %v\n", s, r.MatchString("peach"))
}

// func main() {
//     fmt.Printf("Hello, world.\n")

//     testRexex("pd.peach.money")
// }