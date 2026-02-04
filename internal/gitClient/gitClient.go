package gitClient

import "github.com/Gyt-project/soft-serve/pkg/grpc"

var GitClient *grpc.Client

func InitGitClient(addr string) error {
	client, err := grpc.NewClient(addr)
	if err != nil {
		return err
	}
	GitClient = client
	return nil
}
