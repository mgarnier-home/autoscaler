package utils

import (
	"testing"
)

func TestGetEnvValue_MissingRequired_ReturnsError(t *testing.T) {
	t.Setenv("TEST_REQUIRED_MISSING", "")

	err, got := GetEnvValue("TEST_REQUIRED_MISSING", "default", true)

	if err == nil {
		t.Fatalf("expected error for missing required env variable")
	}

	if got != "default" {
		t.Fatalf("expected default value %q, got %q", "default", got)
	}
}

func TestGetEnvValue_MissingOptional_ReturnsDefault(t *testing.T) {
	t.Setenv("TEST_OPTIONAL_MISSING", "")

	err, got := GetEnvValue("TEST_OPTIONAL_MISSING", 42, false)

	if err != nil {
		t.Fatalf("expected no error for missing optional env variable, got %v", err)
	}

	if got != 42 {
		t.Fatalf("expected default value %d, got %d", 42, got)
	}
}

func TestGetEnvValue_StringValue_ReturnsString(t *testing.T) {
	t.Setenv("TEST_STRING_VALUE", "hello")

	err, got := GetEnvValue("TEST_STRING_VALUE", "default", false)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestGetEnvValue_BoolValue_ReturnsParsedBool(t *testing.T) {
	t.Setenv("TEST_BOOL_VALUE", "true")

	err, got := GetEnvValue("TEST_BOOL_VALUE", false, false)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

func TestGetEnvValue_IntValue_ReturnsParsedInt(t *testing.T) {
	t.Setenv("TEST_INT_VALUE", "123")

	err, got := GetEnvValue("TEST_INT_VALUE", 0, false)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != 123 {
		t.Fatalf("expected %d, got %d", 123, got)
	}
}

func TestGetEnvValue_InvalidBool_ReturnsErrorAndDefault(t *testing.T) {
	t.Setenv("TEST_INVALID_BOOL", "not-a-bool")

	err, got := GetEnvValue("TEST_INVALID_BOOL", true, false)

	if err == nil {
		t.Fatalf("expected error for invalid bool value")
	}

	if got != true {
		t.Fatalf("expected default value true, got %v", got)
	}
}

func TestGetEnvValue_InvalidInt_ReturnsErrorAndDefault(t *testing.T) {
	t.Setenv("TEST_INVALID_INT", "not-an-int")

	err, got := GetEnvValue("TEST_INVALID_INT", 7, false)

	if err == nil {
		t.Fatalf("expected error for invalid int value")
	}

	if got != 7 {
		t.Fatalf("expected default value %d, got %d", 7, got)
	}
}
