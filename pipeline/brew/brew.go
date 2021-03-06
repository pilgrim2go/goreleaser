package brew

import (
	"bytes"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/google/go-github/github"
	"github.com/goreleaser/goreleaser/clients"
	"github.com/goreleaser/goreleaser/context"
	"github.com/goreleaser/goreleaser/sha256sum"
)

// ErrNoDarwin64Build when there is no build for darwin_amd64 (goos doesn't
// contain darwin and/or goarch doesn't contain amd64)
var ErrNoDarwin64Build = errors.New("brew tap requires a darwin amd64 build")

const formulae = `class {{ .Name }} < Formula
  desc "{{ .Desc }}"
  homepage "{{ .Homepage }}"
  url "https://github.com/{{ .Repo }}/releases/download/{{ .Tag }}/{{ .File }}.{{ .Format }}"
  version "{{ .Tag }}"
  sha256 "{{ .SHA256 }}"

  def install
    bin.install "{{ .BinaryName }}"
  end

  {{- if .Caveats }}

  def caveats
    "{{ .Caveats }}"
  end
  {{- end }}
end
`

type templateData struct {
	Name, Desc, Homepage, Repo, Tag, BinaryName, Caveats, File, Format, SHA256 string
}

// Pipe for brew deployment
type Pipe struct{}

// Description of the pipe
func (Pipe) Description() string {
	return "Creating homebrew formulae..."
}

// Run the pipe
func (Pipe) Run(ctx *context.Context) error {
	if ctx.Config.Brew.Repo == "" {
		return nil
	}
	client := clients.GitHub(*ctx.Token)
	path := filepath.Join(
		ctx.Config.Brew.Folder, ctx.Config.Build.BinaryName+".rb",
	)

	log.Println("Updating", path, "on", ctx.Config.Brew.Repo, "...")
	out, err := buildFormulae(ctx, client)
	if err != nil {
		return err
	}

	options := &github.RepositoryContentFileOptions{
		Committer: &github.CommitAuthor{
			Name:  github.String("goreleaserbot"),
			Email: github.String("bot@goreleaser"),
		},
		Content: out.Bytes(),
		Message: github.String(
			ctx.Config.Build.BinaryName + " version " + ctx.Git.CurrentTag,
		),
	}

	owner := ctx.BrewRepo.Owner
	repo := ctx.BrewRepo.Name
	file, _, res, err := client.Repositories.GetContents(
		owner, repo, path, &github.RepositoryContentGetOptions{},
	)
	if err != nil && res.StatusCode == 404 {
		_, _, err = client.Repositories.CreateFile(owner, repo, path, options)
		return err
	}
	options.SHA = file.SHA
	_, _, err = client.Repositories.UpdateFile(owner, repo, path, options)
	return err
}

func buildFormulae(ctx *context.Context, client *github.Client) (bytes.Buffer, error) {
	data, err := dataFor(ctx, client)
	if err != nil {
		return bytes.Buffer{}, err
	}
	return doBuildFormulae(data)
}

func doBuildFormulae(data templateData) (bytes.Buffer, error) {
	var out bytes.Buffer
	tmpl, err := template.New(data.BinaryName).Parse(formulae)
	if err != nil {
		return out, err
	}
	err = tmpl.Execute(&out, data)
	return out, err
}

func dataFor(ctx *context.Context, client *github.Client) (result templateData, err error) {
	var homepage string
	var description string
	rep, _, err := client.Repositories.Get(ctx.ReleaseRepo.Owner, ctx.ReleaseRepo.Name)
	if err != nil {
		return
	}
	file := ctx.Archives["darwinamd64"]
	if file == "" {
		return result, ErrNoDarwin64Build
	}
	sum, err := sha256sum.For("dist/" + file + "." + ctx.Config.Archive.Format)
	if err != nil {
		return
	}
	if rep.Homepage != nil && *rep.Homepage != "" {
		homepage = *rep.Homepage
	} else {
		homepage = *rep.HTMLURL
	}
	if rep.Description == nil {
		description = "TODO"
	} else {
		description = *rep.Description
	}
	return templateData{
		Name:       formulaNameFor(ctx.Config.Build.BinaryName),
		Desc:       description,
		Homepage:   homepage,
		Repo:       ctx.Config.Release.Repo,
		Tag:        ctx.Git.CurrentTag,
		BinaryName: ctx.Config.Build.BinaryName,
		Caveats:    ctx.Config.Brew.Caveats,
		File:       file,
		Format:     ctx.Config.Archive.Format,
		SHA256:     sum,
	}, err
}

func formulaNameFor(name string) string {
	name = strings.Replace(name, "-", " ", -1)
	name = strings.Replace(name, "_", " ", -1)
	return strings.Replace(strings.Title(name), " ", "", -1)
}
