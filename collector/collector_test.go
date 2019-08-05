package collector

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

func TestConfigFile(t *testing.T) {

	badConfigs, err := filepath.Glob("../testdata/config.bad*")
	if err != nil {
		t.Error(err)
	}

	for _, cfg := range badConfigs {
		var conf = Config{}
		f, err := ioutil.ReadFile(cfg)
		if err != nil {
			t.Error(err)
		}

		if err = yaml.UnmarshalStrict(f, &conf); err != nil {
			t.Error("Error on reading: ", cfg)
		}

		if errList := validateConfig(conf); errList == nil {
			t.Error("Did not receive an expected error from: ", cfg)
		}
	}

	goodConfig := "../testdata/config.good.yml"
	var conf = Config{}
	f, err := ioutil.ReadFile(goodConfig)
	if err != nil {
		t.Error(err)
	}

	if err = yaml.UnmarshalStrict(f, &conf); err != nil {
		t.Error("Error unmarshalling config file ", goodConfig, err)
	}

	if errList := validateConfig(conf); errList != nil {
		t.Error("Error validating valid config file ", goodConfig, errList)
	}

}

func validateConfig(conf Config) (err []error) {
	if conf.Link == "" {
		err = append(err, errors.New("Link not defined"))
	}

	if conf.Username == "" {
		err = append(err, errors.New("User not defined"))
	}

	if conf.Password == "" {
		err = append(err, errors.New("Password not defined"))
	}

	return
}

func TestIncluded(t *testing.T) {
	var (
		cfg        = Config{}
		goodConfig = "../testdata/config.good.yml"
	)

	f, err := ioutil.ReadFile(goodConfig)
	if err != nil {
		t.Error(err)
	}
	if err = yaml.UnmarshalStrict(f, &cfg); err != nil {
		t.Error("Error unmarshalling config file ", goodConfig, err)
	}

	a := NewAttunityCollector(&cfg)

	tasks, err := a.taskStates("xprd04")
	if err != nil {
		t.Error(err)
	}
	if len(tasks) > 1 {
		t.Error("More tasks returned than expected. Should only return BIZFLW-WHPRD on xprd04")
	}

}

func TestExcluded(t *testing.T) {
	var (
		cfg        = Config{}
		goodConfig = "../testdata/config.good.yml"
	)

	f, err := ioutil.ReadFile(goodConfig)
	if err != nil {
		t.Error(err)
	}
	if err = yaml.UnmarshalStrict(f, &cfg); err != nil {
		t.Error("Error unmarshalling config file ", goodConfig, err)
	}

	a := NewAttunityCollector(&cfg)

	tasks, err := a.taskStates("LUORAGW02")
	if err != nil {
		t.Error(err)
	}
	for _, x := range tasks {
		if x.Name == "IPCC" {
			t.Error("Excluded task was returned in results.")
		}
	}

}
