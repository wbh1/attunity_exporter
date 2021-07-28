package collector

import (
	"io/ioutil"
	"path/filepath"
	"testing"

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
	cfg.IncludedTags = make([]*string, 0)

	a := NewAttunityCollector(&cfg)

	tasks, err := a.taskStates("xprd03")
	if err != nil {
		t.Error(err)
	}
	if len(tasks) != 1 {
		t.Error("More/less tasks returned than expected. Should only return BIZFLW-WHPRD on xprd03", tasks)
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

	cfg.IncludedTags = make([]*string, 0)

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

func TestTags(t *testing.T) {
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

	tasks, err := a.taskStates("xprd03")
	if err != nil {
		t.Error(err)
	}

	if len(tasks) > 0 {
		t.Error("No tasks should be returned by this tag filter")
		t.Error(tasks)
	}

}
