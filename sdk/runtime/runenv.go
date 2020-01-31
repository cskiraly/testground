package runtime

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type key int

const (
	EnvTestPlan               = "TEST_PLAN"
	EnvTestBranch             = "TEST_BRANCH"
	EnvTestCase               = "TEST_CASE"
	EnvTestTag                = "TEST_TAG"
	EnvTestRun                = "TEST_RUN"
	EnvTestRepo               = "TEST_REPO"
	EnvTestSubnet             = "TEST_SUBNET"
	EnvTestCaseSeq            = "TEST_CASE_SEQ"
	EnvTestSidecar            = "TEST_SIDECAR"
	EnvTestInstanceCount      = "TEST_INSTANCE_COUNT"
	EnvTestInstanceRole       = "TEST_INSTANCE_ROLE"
	EnvTestInstanceParams     = "TEST_INSTANCE_PARAMS"
	EnvTestGroupID            = "TEST_GROUP_ID"
	EnvTestGroupInstanceCount = "TEST_GROUP_INSTANCE_COUNT"
)

// RunEnv encapsulates the context for this test run.
type RunEnv struct {
	TestPlan    string `json:"test_plan"`
	TestCase    string `json:"test_case"`
	TestRun     string `json:"test_run"`
	TestCaseSeq int    `json:"test_seq"`

	TestRepo   string `json:"test_repo,omitempty"`
	TestCommit string `json:"test_commit,omitempty"`
	TestBranch string `json:"test_branch,omitempty"`
	TestTag    string `json:"test_tag,omitempty"`

	TestArtifacts string `json:"test_artifacts,omitempty"`

	TestInstanceCount  int               `json:"test_instance_count"`
	TestInstanceRole   string            `json:"test_instance_role,omitempty"`
	TestInstanceParams map[string]string `json:"test_instance_params,omitempty"`

	TestGroupID            string `json:"test_group_id,omitempty"`
	TestGroupInstanceCount int    `json:"test_group_instance_count,omitempty"`

	// true if the test has access to the sidecar.
	TestSidecar bool `json:"test_sidecar,omitempty"`

	// The subnet on which this test is running.
	//
	// The test instance can use this to pick an IP address and/or determine
	// the "data" network interface.
	//
	// This will be 127.1.0.0/16 when using the local exec runner.
	TestSubnet *net.IPNet `json:"test_network,omitempty"`

	// TODO: we'll want different kinds of loggers.
	logger  *zap.Logger
	slogger *zap.SugaredLogger
}

func (re *RunEnv) ToEnvVars() map[string]string {
	packParams := func(in map[string]string) string {
		arr := make([]string, 0, len(in))
		for k, v := range in {
			arr = append(arr, k+"="+v)
		}
		return strings.Join(arr, "|")
	}

	out := map[string]string{
		EnvTestSidecar:            strconv.FormatBool(re.TestSidecar),
		EnvTestPlan:               re.TestPlan,
		EnvTestBranch:             re.TestBranch,
		EnvTestCase:               re.TestCase,
		EnvTestTag:                re.TestTag,
		EnvTestRun:                re.TestRun,
		EnvTestRepo:               re.TestRepo,
		EnvTestSubnet:             re.TestSubnet.String(),
		EnvTestCaseSeq:            strconv.Itoa(re.TestCaseSeq),
		EnvTestInstanceCount:      strconv.Itoa(re.TestInstanceCount),
		EnvTestInstanceRole:       re.TestInstanceRole,
		EnvTestInstanceParams:     packParams(re.TestInstanceParams),
		EnvTestGroupID:            re.TestGroupID,
		EnvTestGroupInstanceCount: strconv.Itoa(re.TestGroupInstanceCount),
	}

	return out
}

func (re *RunEnv) SLogger() *zap.SugaredLogger {
	if re.slogger == nil {
		re.initLoggers()
	}
	return re.slogger
}

// Loggers returns the loggers populated from this runenv.
func (re *RunEnv) Loggers() (*zap.Logger, *zap.SugaredLogger) {
	if re.logger == nil || re.slogger == nil {
		re.initLoggers()
	}
	return re.logger, re.slogger
}

// initLoggers populates loggers from this RunEnv.
func (re *RunEnv) initLoggers() {
	cfg := zap.NewDevelopmentConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	cfg.Encoding = "json"

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		l := zapcore.Level(0)
		l.UnmarshalText([]byte(level))
		cfg.Level = zap.NewAtomicLevelAt(l)
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	re.logger = logger.With(
		zap.String("plan", re.TestPlan),
		zap.String("case", re.TestCase),
		zap.String("run", re.TestRun),
		zap.Int("seq", re.TestCaseSeq),
		zap.String("repo", re.TestRepo),
		zap.String("commit", re.TestCommit),
		zap.String("branch", re.TestBranch),
		zap.String("tag", re.TestTag),
		zap.Int("instances", re.TestInstanceCount),
		zap.String("group", re.TestGroupID),
	)
	re.slogger = re.logger.Sugar()
}

func unpackParams(packed string) map[string]string {
	spltparams := strings.Split(packed, "|")
	params := make(map[string]string, len(spltparams))
	for _, s := range spltparams {
		v := strings.Split(s, "=")
		if len(v) != 2 {
			continue
		}
		params[v[0]] = v[1]
	}
	return params
}

func toInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return v
}

func toBool(s string) bool {
	v, _ := strconv.ParseBool(s)
	return v
}

func toNet(s string) *net.IPNet {
	_, ipnet, _ := net.ParseCIDR(s)
	return ipnet
}

// CurrentRunEnv populates a test context from environment vars.
func CurrentRunEnv() *RunEnv {
	re := &RunEnv{
		TestSidecar:            toBool(os.Getenv(EnvTestSidecar)),
		TestPlan:               os.Getenv(EnvTestPlan),
		TestCase:               os.Getenv(EnvTestCase),
		TestRun:                os.Getenv(EnvTestRun),
		TestTag:                os.Getenv(EnvTestTag),
		TestBranch:             os.Getenv(EnvTestBranch),
		TestRepo:               os.Getenv(EnvTestRepo),
		TestSubnet:             toNet(os.Getenv(EnvTestSubnet)),
		TestCaseSeq:            toInt(os.Getenv(EnvTestCaseSeq)),
		TestInstanceCount:      toInt(os.Getenv(EnvTestInstanceCount)),
		TestInstanceRole:       os.Getenv(EnvTestInstanceRole),
		TestInstanceParams:     unpackParams(os.Getenv(EnvTestInstanceParams)),
		TestGroupID:            os.Getenv(EnvTestGroupID),
		TestGroupInstanceCount: toInt(os.Getenv(EnvTestGroupInstanceCount)),
	}

	re.initLoggers()

	return re
}

// ParseRunEnv parses a list of environment variables into a RunEnv.
func ParseRunEnv(env []string) (*RunEnv, error) {
	m, err := ParseKeyValues(env)
	if err != nil {
		return nil, err
	}

	re := &RunEnv{
		TestSidecar:        toBool(m[EnvTestSidecar]),
		TestPlan:           m[EnvTestPlan],
		TestCase:           m[EnvTestCase],
		TestRun:            m[EnvTestRun],
		TestTag:            m[EnvTestTag],
		TestBranch:         m[EnvTestBranch],
		TestRepo:           m[EnvTestRepo],
		TestSubnet:         toNet(m[EnvTestSubnet]),
		TestCaseSeq:        toInt(m[EnvTestCaseSeq]),
		TestInstanceCount:  toInt(m[EnvTestInstanceCount]),
		TestInstanceRole:   m[EnvTestInstanceRole],
		TestInstanceParams: unpackParams(m[EnvTestInstanceParams]),
	}

	re.initLoggers()

	return re, nil
}

// IsParamSet checks if a certain parameter is set.
func (re *RunEnv) IsParamSet(name string) bool {
	_, ok := re.TestInstanceParams[name]
	return ok
}

// StringParam returns a string parameter, or "" if the parameter is not set.
func (re *RunEnv) StringParam(name string) string {
	v, ok := re.TestInstanceParams[name]
	if !ok {
		panic(fmt.Errorf("%s was not set", name))
	}
	return v
}

func (re *RunEnv) SizeParam(name string) uint64 {
	v, _ := re.TestInstanceParams[name]
	m, err := humanize.ParseBytes(v)
	if err != nil {
		panic(err)
	}
	return m
}

// IntParam returns an int parameter, or -1 if the parameter is not set or
// the conversion failed. It panics on error.
func (re *RunEnv) IntParam(name string) int {
	v, ok := re.TestInstanceParams[name]
	if !ok {
		panic(fmt.Errorf("%s was not set", name))
	}

	i, err := strconv.Atoi(v)
	if err != nil {
		panic(err)
	}
	return i
}

// BooleanParam returns the Boolean value of the parameter, or false if not passed
func (re *RunEnv) BooleanParam(name string) bool {
	s, _ := re.TestInstanceParams[name]
	if s == "true" {
		return true
	}
	return false
}

// StringArrayParam returns an array of string parameter, or an empty array
// if it does not exist. It panics on error.
func (re *RunEnv) StringArrayParam(name string) []string {
	a := []string{}
	re.JSONParam(name, &a)
	return a
}

// SizeArrayParam returns an array of uint64 elements which represent sizes,
// in bytes. If the response is nil, then there was an error parsing the input.
// It panics on error.
func (re *RunEnv) SizeArrayParam(name string) []uint64 {
	humanSizes := re.StringArrayParam(name)
	sizes := []uint64{}

	for _, size := range humanSizes {
		n, err := humanize.ParseBytes(size)
		if err != nil {
			panic(err)
		}
		sizes = append(sizes, n)
	}

	return sizes
}

// JSONParam unmarshals a JSON parameter in an arbitrary interface.
// It panics on error.
func (re *RunEnv) JSONParam(name string, v interface{}) {
	s, ok := re.TestInstanceParams[name]
	if !ok {
		panic(fmt.Errorf("%s was not set", name))
	}

	if err := json.Unmarshal([]byte(s), v); err != nil {
		panic(err)
	}
}

// RandomRunEnv generates a random RunEnv for testing purposes.
func RandomRunEnv() *RunEnv {
	b := make([]byte, 32)
	_, _ = rand.Read(b)

	return &RunEnv{
		TestPlan:           fmt.Sprintf("testplan-%d", rand.Uint32()),
		TestSidecar:        false,
		TestCase:           fmt.Sprintf("testcase-%d", rand.Uint32()),
		TestRun:            fmt.Sprintf("testrun-%d", rand.Uint32()),
		TestCaseSeq:        int(rand.Uint32()),
		TestRepo:           "github.com/ipfs/go-ipfs",
		TestSubnet:         toNet("127.1.0.1/16"),
		TestCommit:         fmt.Sprintf("%x", sha1.Sum(b)),
		TestInstanceCount:  int(1 + (rand.Uint32() % 999)),
		TestInstanceRole:   "",
		TestInstanceParams: make(map[string]string, 0),
	}
}

// Copied from github.com/ipfs/testground/pkg/conv, because we don't want the
// SDK to depend on that package.
func ParseKeyValues(in []string) (res map[string]string, err error) {
	res = make(map[string]string, len(in))
	for _, d := range in {
		splt := strings.Split(d, "=")
		if len(splt) != 2 {
			return nil, fmt.Errorf("invalid key-value: %s", d)
		}
		res[splt[0]] = splt[1]
	}
	return res, nil
}
