package utils

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func InitEnvFromFile() {
	err, envFilePath := GetEnvValue("ENV_FILE_PATH", "./.env", false)

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}

	exPath := path.Dir(ex)

	if !path.IsAbs(envFilePath) {
		envFilePath = path.Join(exPath, envFilePath)
	}

	godotenv.Load(envFilePath)
}

func GetEnvValue[T bool | string | int | []string](key string, defaultValue T, required bool) (error, T) {
	value := os.Getenv(key)

	if value == "" {
		if _, err := os.Stat("/run/secrets/" + strings.ToLower(key)); err == nil {
			fileContent, err := os.ReadFile("/run/secrets/" + strings.ToLower(key))

			if err != nil {
				value = ""
			} else {
				value = string(fileContent)
			}
		}
	}

	if value == "" {
		if required {
			return fmt.Errorf("Required environment variable %s is not set", key), defaultValue
		}
		return nil, defaultValue
	}

	switch any(defaultValue).(type) {
	case bool:
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return nil, any(boolValue).(T)
		}
	case int:
		if intValue, err := strconv.Atoi(value); err == nil {
			return nil, any(intValue).(T)
		}
	case string:
		return nil, any(value).(T)
	case []string:
		return nil, any(strings.Split(value, ",")).(T)
	}

	return fmt.Errorf("Environment variable %s has an invalid type", key), defaultValue
}
