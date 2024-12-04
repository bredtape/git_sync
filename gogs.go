package git_sync

import (
	"math/rand/v2"

	api "github.com/gogs/go-gogs-client"
	"github.com/pkg/errors"
)

type GogsAdmin struct {
	user, password, baseURL string
}

func NewGogsAdmin(user, password, baseURL string) *GogsAdmin {
	return &GogsAdmin{user, password, baseURL}
}

func (g *GogsAdmin) getGogsAPIClient() (string, *api.Client, error) {
	client := api.NewClient(g.baseURL, "")
	token, err := client.CreateAccessToken(g.user, g.password,
		api.CreateAccessTokenOption{Name: generateRandomString()})
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to create access token")
	}
	return token.Sha1, api.NewClient(g.baseURL, token.Sha1), nil
}

type RemoteRepo struct {
	Name  string
	URL   string
	Token string
}

func (g *GogsAdmin) CreateRandomRepo() (RemoteRepo, error) {
	token, client, err := g.getGogsAPIClient()
	if err != nil {
		return RemoteRepo{}, errors.Wrap(err, "failed to create client with access token")
	}

	repoOpts := api.CreateRepoOption{
		Name:        generateRandomString(),
		Description: "Test repository"}

	repo, err := client.CreateRepo(repoOpts)
	if err != nil {
		return RemoteRepo{}, errors.Wrap(err, "failed to create repo")
	}

	return RemoteRepo{
		Name:  repo.Name,
		URL:   repo.CloneURL,
		Token: token,
	}, nil
}

func generateRandomString() string {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 10)
	for i := range b {
		b[i] = charset[rand.IntN(len(charset))]
	}
	return string(b)
}
