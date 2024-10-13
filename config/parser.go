package config
import (
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"os"
)

// config represents the structure of the yaml configuration
type Config struct {
    ProxyServer     struct {
        Address string `yaml:"address"`
    } `yaml:"proxy_server"`
	Serverpool      []string `yaml:"backend_servers"`
	Workerprocesses int      `yaml:"worker_processes"`
	Routing         struct {
		Strategy string `yaml:"strategy"`
	} `yaml:"routing"`
	Healthcheck struct {
		Enabled  bool `yaml:"enabled"`
		Interval int  `yaml:"interval"`
		Timeout  int  `yaml:"timeout"`
	} `yaml:"health_check"`
	Logging struct {
		Level string `yaml:"level"`
		File  string `yaml:"file"`
	} `yaml:"logging"`
}

func LoadConfig(filePath string) (*Config, error) {
	// Open the YAML file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read the YAML file into a byte slice
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// Parse the YAML into the Config struct
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
