package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/AlecAivazis/survey/v2"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/fatih/color"
)

var (
	region   string
	endpoint string

	red    = color.New(color.FgRed).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
)

func init() {
	client := createEcsClient()
	region = client.SigningRegion
	endpoint = client.Endpoint
}

func createEcsClient() *ecs.ECS {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	client := ecs.New(sess)

	return client
}

type ECSListClustersAPI interface {
	ListClusters(input *ecs.ListClustersInput) (*ecs.ListClustersOutput, error)
}

// Lists available clusters and prompts the user to select one
func getCluster(client ECSListClustersAPI) (string, error) {
	list, err := client.ListClusters(&ecs.ListClustersInput{})
	if err != nil {
		return "", err
	}

	var clusterName string
	if len(list.ClusterArns) > 0 {
		var clusterNames []string
		for _, c := range list.ClusterArns {
			arnSplit := strings.Split(*c, "/")
			name := arnSplit[len(arnSplit)-1]
			clusterNames = append(clusterNames, name)
		}
		selection, err := selectCluster(clusterNames)
		if err != nil {
			return "", err
		}
		clusterName = selection
		return clusterName, nil
	} else {
		err := errors.New("No clusters found in account or region")
		return "", err
	}
}

// Lists tasks in a cluster and prompts the user to select one
func getTask(client *ecs.ECS, clusterName string) (*ecs.Task, error) {
	list, err := client.ListTasks(&ecs.ListTasksInput{
		Cluster: aws.String(clusterName),
	})
	if err != nil {
		return nil, err
	}
	if len(list.TaskArns) > 0 {
		describe, err := client.DescribeTasks(&ecs.DescribeTasksInput{
			Cluster: aws.String(clusterName),
			Tasks:   list.TaskArns,
		})
		if err != nil {
			return nil, err
		}
		// Ask the user to select which Task to connect to
		selection, err := selectTask(describe.Tasks)
		if err != nil {
			return nil, err
		}
		task := selection
		return task, nil
	} else {
		err := errors.New(fmt.Sprintf("There are no running tasks in the cluster %s", clusterName))
		return nil, err
	}
}

// Lists containers in a task and prompts the user to select one (if there is more than 1 container)
// otherwise returns the the only container in the task
func getContainer(client *ecs.ECS, task *ecs.Task) (*ecs.Container, error) {
	if len(task.Containers) > 1 {
		// Ask the user to select a container
		selection, err := selectContainer(task.Containers)
		if err != nil {
			return &ecs.Container{}, err
		}
		return selection, nil
	} else {
		// There is only one container in the task, return it
		return task.Containers[0], nil
	}
}

// selectCluster provides the prompt for choosing a cluster
func selectCluster(clusterNames []string) (string, error) {
	var clusterName string
	var qs = []*survey.Question{
		{
			Name: "Cluster",
			Prompt: &survey.Select{
				Message: "Cluster your task resides in:",
				Options: clusterNames,
			},
		},
	}

	err := survey.Ask(qs, &clusterName)
	if err != nil {
		fmt.Println(err.Error())
		return "", err
	}

	return clusterName, nil
}

// selectTask provides the prompt for choosing a Task
func selectTask(tasks []*ecs.Task) (*ecs.Task, error) {
	var options []string
	for _, t := range tasks {
		var containers []string
		for _, c := range t.Containers {
			containers = append(containers, *c.Name)
		}
		id := strings.Split(*t.TaskArn, "/")[2]
		taskDefinion := strings.Split(*t.TaskDefinitionArn, "/")[1]
		options = append(options, fmt.Sprintf("%s\t%s\t(%s)", id, taskDefinion, strings.Join(containers, ",")))
	}

	var qs = []*survey.Question{
		{
			Name: "Task",
			Prompt: &survey.Select{
				Message: "Task you would like to connect to:",
				Options: options,
			},
		},
	}

	var selection string
	err := survey.Ask(qs, &selection)
	if err != nil {
		fmt.Println(err.Error())
		return &ecs.Task{}, err
	}

	var task *ecs.Task
	// Loop through our tasks and pull out the one which matches our selection
	for _, t := range tasks {
		id := strings.Split(*t.TaskArn, "/")[2]
		if strings.Contains(selection, id) {
			task = t
			break
		}
	}

	return task, nil
}

// selectContainer prompts the user to choose a container within a task
func selectContainer(containers []*ecs.Container) (*ecs.Container, error) {
	var containerNames []string
	for _, c := range containers {
		containerNames = append(containerNames, *c.Name)
	}

	var selection string
	var qs = []*survey.Question{
		{
			Name: "Container",
			Prompt: &survey.Select{
				Message: "More than one container in task, please choose the one you would like to connect to:",
				Options: containerNames,
			},
		},
	}

	err := survey.Ask(qs, &selection)
	if err != nil {
		fmt.Println(err.Error())
		return &ecs.Container{}, err
	}

	var container *ecs.Container
	for _, c := range containers {
		if strings.Contains(*c.Name, selection) {
			container = c
		}
	}

	return container, nil
}

// runCommand executes a command with args, catches any signals and handles them -
// not to be consufed
func runCommand(process string, args ...string) error {
	cmd := exec.Command(process, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, os.Kill, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for {
			select {
			case <-sigs:
				os.Exit(0)
			}
		}
	}()
	defer close(sigs)

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

// selectProfile
/* func selectProfile(profiles []string) (string, error) {
	var profile string
	var qs = []*survey.Question{
		{
			Name: "profile",
			Prompt: &survey.Select{
				Message: "Select your AWS Profile",
				Options: profiles,
			},
		},
	}
	// perform the questions
	err := survey.Ask(qs, &profile)
	if err != nil {
		fmt.Println(err.Error())
		return "", err
	}

	return profile, nil
}
*/

// ReadAwsConfig reads in the aws config file and returns a slice of all profile names
/* func ReadAwsConfig() ([]string, error) {
	// Find home directory.
	home, err := homedir.Dir()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	data, err := ioutil.ReadFile(fmt.Sprintf("%s/.aws/config", home))
	if err != nil {
		log.Fatal(err)
	}

	var profiles []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Index(line, "[profile ") > -1 {
			raw := strings.Split(line, " ")[1]
			profile := strings.TrimRight(raw, "]")
			profiles = append(profiles, profile)
		}
	}

	return profiles, nil
}
*/