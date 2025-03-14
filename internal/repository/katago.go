package repository

import (
	"bufio"
	"fmt"
	"go.uber.org/zap"
	"os/exec"
	"sync"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain"
)

type KatagoRepository struct {
	cfg    *bootstrap.Config
	log    *zap.SugaredLogger
	client *KatagoClient
}

type KatagoClient struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	mu     sync.Mutex
}

func NewKatagoRepository(cfg *bootstrap.Config, log *zap.SugaredLogger) *KatagoRepository {
	return &KatagoRepository{
		cfg: cfg,
		log: log,
	}
}

func NewKatagoClient() (*KatagoClient, error) {
	cmd := exec.Command("./katago", "analysis", "-model", "kata1-b40c256-s11840935168-d2898845681.bin", "-config", "gtp_custom.cfg")
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	client := &KatagoClient{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdinPipe),
		stdout: bufio.NewScanner(stdoutPipe),
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go client.listenForResponses()

	return client, nil
}

func (c *KatagoClient) listenForResponses() {
	for c.stdout.Scan() {
		line := c.stdout.Text()
		fmt.Println("KataGo response:", line)
	}
}

func (k *KatagoRepository) StartGame(gameConfig domain.KatagoGameStartRequest) {
	// ./katago gtp -model kata1-b40c256-s11840935168-d2898845681.bin -config gtp_custom.cfg
}

func (k *KatagoRepository) StartNewKatagoServer() {

}
