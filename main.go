package main

import (
	"fmt"
	"os"
	"encoding/base64"
	"io/ioutil"
	"encoding/json"
	"path"
	"github.com/bitrise-io/go-utils/command"
	"strings"
	"time"
	"math/rand"
	"github.com/kballard/go-shellquote"
	"os/exec"
	"github.com/bitrise-tools/gows/config"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// Matches api_lib/firebase/test/arg_validate.py _GenerateUniqueGcsObjectName from gcloud SDK
// Example output: 2017-07-12_11:36:12.467586_XVlB
func gcsObjectName() string {
	letterCount := 4
	bytes := make([]byte, letterCount)

	for i := 0; i < letterCount; i++ {
		bytes[i] = letters[rand.Intn(len(letters))]
	}

	return time.Now().Format("2006-01-02_3:04:05.999999") + "_" + string(bytes)
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func getOptionalEnv(env string) string {
	return os.Getenv(env)
}

func getRequiredEnv(env string) string {
	if len(env) == 0 {
		panic(env + " is not defined!")
	}

	return os.Getenv(env)
}

func isEmpty(str string) bool {
	return len(str) == 0
}

func checkFileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

type GcloudKeyFile struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
}

func runCommand(cmd string) {
	cmdSlice := strings.Fields(cmd)

	cmdObj := command.NewWithStandardOuts(cmdSlice[0], cmdSlice[1:]...)
	checkError(cmdObj.Run())
}

// Env string names
const GCLOUD_USER = "GCLOUD_USER" // optional. read from keyfile
const GCLOUD_PROJECT = "GCLOUD_PROJECT" // optional. read from keyfile
const GCLOUD_BUCKET = "GCLOUD_BUCKET" // required
const GCLOUD_OPTIONS = "GCLOUD_OPTIONS" // required
const APP_APK = "APP_APK" // required
const TEST_APK = "TEST_APK" // optional
const GCLOUD_KEY = "GCLOUD_KEY" // required

// Output from the step
const GCS_RESULTS_DIR = "GCS_RESULTS_DIR"

type FirebaseConfig struct {
	ResultsBucket string
	Options       string
	User          string
	Project       string
	KeyPath       string
	AppApk        string
	TestApk       string
}

func populateConfig() FirebaseConfig {
	gcloud_user := getOptionalEnv(GCLOUD_USER)
	gcloud_project := getOptionalEnv(GCLOUD_PROJECT)

	app_apk := getRequiredEnv(APP_APK)
	checkFileExists(app_apk)

	test_apk := getOptionalEnv(TEST_APK)
	if !isEmpty(test_apk) {
		checkFileExists(test_apk)
	}

	gcloud_key, err := base64.StdEncoding.DecodeString(getRequiredEnv(GCLOUD_KEY))
	checkError(err)

	empty_gcloud_user := isEmpty(gcloud_user)
	empty_gcloud_project := isEmpty(gcloud_project)

	if empty_gcloud_user || empty_gcloud_project {
		parsedKeyFile := GcloudKeyFile{}
		json.Unmarshal([]byte(gcloud_key), &parsedKeyFile)

		if empty_gcloud_user {
			gcloud_user = parsedKeyFile.ClientEmail
			if isEmpty(gcloud_user) {
				panic("Missing gcloud user")
			}
		}

		if empty_gcloud_project {
			gcloud_project = parsedKeyFile.ProjectID
			if isEmpty(gcloud_project) {
				panic("Missing gcloud project")
			}
		}
	}

	home_dir := getRequiredEnv("HOME")
	key_file_path := path.Join(home_dir, "gcloudkey.json")
	checkError(ioutil.WriteFile(key_file_path, gcloud_key, 0644))

	return FirebaseConfig{
		ResultsBucket: getRequiredEnv(GCLOUD_BUCKET),
		User:          gcloud_user,
		Project:       gcloud_project,
		KeyPath:       key_file_path,
		AppApk:        app_apk,
		TestApk:       test_apk,
		Options:       getRequiredEnv(GCLOUD_OPTIONS),
	}
}


func exportGcsDir(bucket string, object string) {
	gcs_results_dir := "gs://" +bucket + "/" + object
	fmt.Println("Exporting ", GCS_RESULTS_DIR, " ", gcs_results_dir)
	cmdLog, err := exec.Command("bitrise", "envman", "add", "--key", GCS_RESULTS_DIR, "--value", gcs_results_dir).CombinedOutput()
	if err != nil {
		fmt.Printf("Failed to export "+GCS_RESULTS_DIR+", error: %#v | output: %s", err, cmdLog)
		os.Exit(1)
	}
}

func main() {
	config := populateConfig()
	fmt.Println("Config: ", config)

	runCommand("gcloud config set project " + config.Project)
	runCommand("gcloud auth activate-service-account --key-file " + config.KeyPath + " " + config.User)

	// todo: input variable support
	// https://cloud.google.com/sdk/gcloud/reference/firebase/test/android/run
	gcloudOptions, err := shellquote.Split(config.Options)
	checkError(err)
	fmt.Println("user options: ", gcloudOptions)

	// todo: may require all args to be supplied by user
	args := make([]string, 0)

	test_type := "robo"

	if !isEmpty(config.TestApk) {
		test_type = "instrumentation"
		args = append(args, "--test", config.TestApk)
		args = append(args, "--directories-to-pull=/sdcard")
	}

	args = append(args, test_type)
	args = append(args, "--app", config.AppApk)
	args = append(args, "--device-ids", "NexusLowRes")
	args = append(args, "--os-version-ids", "25")
	args = append(args, "--locales", "en")
	args = append(args, "--orientations", "portrait")
	args = append(args, "--timeout", "25m")
	args = append(args, "--results-bucket="+config.ResultsBucket)
	gcs_object := gcsObjectName()
	args = append(args, "--results-dir="+gcs_object)

	fmt.Println("args: ", args)

	exportGcsDir(config.ResultsBucket, gcs_object)

	os.Exit(0)
}
