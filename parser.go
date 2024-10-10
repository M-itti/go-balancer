package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"os"
)

// Config represents the structure of the YAML configuration
type Config struct {
	ServerPool      []string `yaml:"server_pool"`
	WorkerProcesses int      `yaml:"worker_processes"`
	ListenPort      int      `yaml:"listen_port"`
	Routing         struct {
		Strategy string `yaml:"strategy"`
	} `yaml:"routing"`
	HealthCheck struct {
		Enabled  bool `yaml:"enabled"`
		Interval int  `yaml:"interval"`
		Timeout  int  `yaml:"timeout"`
	} `yaml:"health_check"`
	Logging struct {
		Level string `yaml:"level"`
		File  string `yaml:"file"`
	} `yaml:"logging"`
}

func main() {
	// Open the YAML file
	file, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("Error opening YAML file: %s", err)
	}
	defer file.Close()

	// Read the YAML file into a byte slice
	data, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Error reading YAML file: %s", err)
	}

	// Parse the YAML into the Config struct
	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Error parsing YAML file: %s", err)
	}

	// Output the parsed YAML data
	fmt.Println("Server Pool:")
	for _, server := range config.ServerPool {
		fmt.Printf("  - %s\n", server)
	}
	fmt.Printf("Worker Processes: %d\n", config.WorkerProcesses)
	fmt.Printf("Listen Port: %d\n", config.ListenPort)
	fmt.Printf("Routing Strategy: %s\n", config.Routing.Strategy)
	fmt.Printf("Health Check Enabled: %t\n", config.HealthCheck.Enabled)
	fmt.Printf("Health Check Interval: %d\n", config.HealthCheck.Interval)
	fmt.Printf("Health Check Timeout: %d\n", config.HealthCheck.Timeout)
	fmt.Printf("Logging Level: %s\n", config.Logging.Level)
	fmt.Printf("Logging File: %s\n", config.Logging.File)
}
