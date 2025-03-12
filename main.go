package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v2"
)

// Config 结构体用于保存配置
type Config struct {
	Registry struct {
		Domain   string `yaml:"domain"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
		Project  string `yaml:"project"`
	} `yaml:"registry"`
	DockerRegistries []string `yaml:"dockerRegistries"`
}

// GetConfigPath 返回配置文件的路径
func GetConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("获取用户主目录出错: %v", err)
	}
	configDir := filepath.Join(homeDir, ".config", "docker-mirror")
	os.MkdirAll(configDir, 0755) // 创建配置目录
	return filepath.Join(configDir, "config.yaml")
}

// LoadConfig 从 YAML 文件加载配置
func LoadConfig(configFile string) (*Config, error) {
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// SaveConfig 将配置保存到 YAML 文件
func SaveConfig(configFile string, config *Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(configFile, data, 0644)
}

// Execute 执行一个 shell 命令并返回其输出
func Execute(command string, args ...string) (string, error) {
	if command == "docker" && len(args) > 0 && args[0] == "pull" && runtime.GOOS == "darwin" {
		args = append(args[:1], append([]string{"--platform", "linux/amd64"}, args[1:]...)...)
	}
	fmt.Println("------>", command, strings.Join(args, " "))
	cmd := exec.Command(command, args...)

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// Prompt 提示用户输入
func Prompt(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// Configure 通过提示用户输入来配置工具
func Configure(configFile string) error {
	config := &Config{}

	config.Registry.Domain = Prompt("请输入 registry 域名: ")
	config.Registry.Username = Prompt("请输入 registry 用户名: ")
	config.Registry.Password = Prompt("请输入 registry 密码: ")

	// 预设 DockerRegistries 的默认值
	config.DockerRegistries = []string{
		"docker.m.daocloud.io",
		"quay.m.daocloud.io",
		"k8s.m.daocloud.io",
	}

	return SaveConfig(configFile, config)
}

// PrintHelp 打印帮助信息
func PrintHelp() {
	fmt.Println("用法: docker-mirror <command> [image]")
	fmt.Println("eg: docker pull bitnami/postgresql:11.14.0-debian-10-r22")
	fmt.Println("")
	fmt.Println("command:")
	fmt.Println("")
	fmt.Println("  config       初始化配置")
	fmt.Println("")
	fmt.Println("  pull         拉取镜像到本地，并推送到 registry 仓库")
	fmt.Println("               注意: 请不要在镜像名称中添加域名")
	fmt.Println("")
	fmt.Println("  pull-local   仅拉取镜像到本地，不推送到 registry 仓库")
	fmt.Println("               注意: 请不要在镜像名称中添加域名")
	fmt.Println("")
	fmt.Println("  help         显示帮助信息")
}

func main() {
	if len(os.Args) < 2 {
		PrintHelp()
		return
	}

	command := os.Args[1]
	configPath := GetConfigPath()

	switch command {
	case "config":
		if err := Configure(configPath); err != nil {
			log.Fatalf("配置出错: %v", err)
		}
		fmt.Println("配置保存成功。")
	case "pull":
		if len(os.Args) != 3 {
			fmt.Println("用法: docker-mirror pull <镜像>")
			return
		}

		image := os.Args[2]
		sourceImage := image
		part := strings.Split(image, "/")

		// 加载配置
		config, err := LoadConfig(configPath)
		if err != nil {
			log.Fatalf("加载配置出错: %v", err)
		}

		// 如果镜像名称中没有斜杠，则默认视为 library/镜像名称
		if len(part) == 1 {
			part = append([]string{"library"}, part[0])
		}
		targetImage := fmt.Sprintf("%s/%s", config.Registry.Domain, image)

		var pullErr error
		var pulledRegistry string
		if len(config.DockerRegistries) == 0 {
			// 如果 DockerRegistries 为空，则直接拉取不带域名的镜像
			fmt.Printf("正在拉取镜像 %s\n", sourceImage)
			if output, err := Execute("docker", "pull", sourceImage); err != nil {
				fmt.Printf("拉取镜像出错: %v\n%s", err, output)
				pullErr = err
			} else {
				pullErr = nil
				pulledRegistry = ""
			}
		} else {
			for _, registry := range config.DockerRegistries {
				// 从配置的 Docker 镜像仓库地址拉取镜像
				fmt.Printf("正在从 %s 拉取镜像 %s\n", registry, sourceImage)
				if output, err := Execute("docker", "pull", fmt.Sprintf("%s/%s", registry, sourceImage)); err != nil {
					fmt.Printf("拉取镜像出错: %v\n%s", err, output)
					pullErr = err
				} else {
					pullErr = nil
					pulledRegistry = registry
					break
				}
			}
		}

		if pullErr != nil {
			log.Fatalf("从所有配置的 DockerRegistry 拉取镜像均失败")
		}

		// 将镜像标记为目标域名
		if pulledRegistry != "" {
			sourceImage = fmt.Sprintf("%s/%s", pulledRegistry, sourceImage)
		}
		fmt.Printf("正在将镜像 %s 标记为 %s\n", sourceImage, targetImage)
		if output, err := Execute("docker", "tag", sourceImage, targetImage); err != nil {
			log.Fatalf("标记镜像出错: %v\n%s", err, output)
		}

		// 登录到 registry 仓库
		fmt.Printf("正在登录到 registry 仓库 %s\n", config.Registry.Domain)
		if output, err := Execute("docker", "login", config.Registry.Domain, "-u", config.Registry.Username, "-p", config.Registry.Password); err != nil {
			log.Fatalf("登录 registry 出错: %v\n%s", err, output)
		}

		// 推送镜像到 registry 仓库
		fmt.Printf("正在推送镜像 %s\n", targetImage)
		if output, err := Execute("docker", "push", targetImage); err != nil {
			log.Fatalf("推送镜像出错: %v\n%s", err, output)
		}

		fmt.Println("镜像成功同步！")
	case "pull-local":
		if len(os.Args) != 3 {
			fmt.Println("用法: docker-mirror pull-local <镜像>")
			return
		}

		image := os.Args[2]
		sourceImage := image

		// 加载配置
		config, err := LoadConfig(configPath)
		if err != nil {
			log.Fatalf("加载配置出错: %v", err)
		}

		var pullErr error
		if len(config.DockerRegistries) == 0 {
			// 如果 DockerRegistries 为空，则直接拉取不带域名的镜像
			fmt.Printf("正在拉取镜像 %s\n", sourceImage)
			if output, err := Execute("docker", "pull", sourceImage); err != nil {
				fmt.Printf("拉取镜像出错: %v\n%s", err, output)
				pullErr = err
			} else {
				pullErr = nil
			}
		} else {
			for _, registry := range config.DockerRegistries {
				// 从配置的 Docker 镜像仓库地址拉取镜像
				fmt.Printf("正在从 %s 拉取镜像 %s\n", registry, sourceImage)
				if output, err := Execute("docker", "pull", fmt.Sprintf("%s/%s", registry, sourceImage)); err != nil {
					fmt.Printf("拉取镜像出错: %v\n%s", err, output)
					pullErr = err
				} else {
					pullErr = nil
					break
				}
			}
		}

		if pullErr != nil {
			log.Fatalf("从所有配置的 DockerRegistry 拉取镜像均失败")
		}

		fmt.Println("您的镜像已成功拉取到本地！")
	case "help":
		PrintHelp()
	default:
		fmt.Println("unknown:", command)
		PrintHelp()
	}
}
