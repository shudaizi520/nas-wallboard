package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const credentialSchemaVersion = 1

var ErrInvalidUsername = errors.New("username must contain 3 to 32 letters, numbers, periods, underscores, or hyphens")

type credentialRecord struct {
	SchemaVersion int    `json:"schema_version"`
	Username      string `json:"username"`
	Password      Record `json:"password"`
}

func ValidateUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if !utf8.ValidString(value) || count < 3 || count > 32 {
		return "", ErrInvalidUsername
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return "", ErrInvalidUsername
	}
	return value, nil
}

func decodeCredentialRecord(data []byte) (username string, password Record, legacy bool, err error) {
	var fields map[string]json.RawMessage
	if decodeErr := decodeStrictJSON(data, &fields); decodeErr != nil {
		return "", Record{}, false, errors.New("auth record is invalid")
	}
	if _, versioned := fields["schema_version"]; versioned {
		var stored credentialRecord
		if decodeErr := decodeStrictJSON(data, &stored); decodeErr != nil || stored.SchemaVersion != credentialSchemaVersion || !validRecord(stored.Password) {
			return "", Record{}, false, errors.New("auth record is invalid")
		}
		validated, validateErr := ValidateUsername(stored.Username)
		if validateErr != nil || validated != stored.Username {
			return "", Record{}, false, errors.New("auth record is invalid")
		}
		return stored.Username, stored.Password, false, nil
	}

	var old Record
	if decodeErr := decodeStrictJSON(data, &old); decodeErr != nil || !validRecord(old) {
		return "", Record{}, false, errors.New("auth record is invalid")
	}
	return "admin", old, true, nil
}

func encodeCredentialRecord(username string, password Record) ([]byte, error) {
	data, err := json.MarshalIndent(credentialRecord{
		SchemaVersion: credentialSchemaVersion,
		Username:      username,
		Password:      password,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
