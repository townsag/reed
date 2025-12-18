package server

import (
	"strings"
	"fmt"
)

func containsSpecialChar(s string) bool {
    specialChars := "!@#$%^&*()_+-=[]{};':\"\\|,.<>/?"
    return strings.ContainsAny(s, specialChars)
}

func (rb *PostUserJSONRequestBody) Validate() error {
	// validate that the password contains a special character using regex
	if !containsSpecialChar(rb.Password) {
		return fmt.Errorf("password did not contain a special character")
	}
	// validate that the username does not include any banned characters
	// for usernames
	if containsSpecialChar(rb.UserName) {
		return fmt.Errorf("username must not contain a special character")
	}
	return nil
}