package utils

import (
	"os"
	"path"

	"github.com/joho/godotenv"

	"github.com/mgarnier-home/utils/common"
)

func InitEnvFromFile() {
	err, envFilePath := common.GetEnvValue("ENV_FILE_PATH", "./.env", false)

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
