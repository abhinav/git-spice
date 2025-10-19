package submit

import (
	"cmp"
	"context"
	"errors"
	"os"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
)

type branchSubmitForm struct {
	ctx    context.Context
	svc    Service
	repo   GitRepository
	remote forge.Repository
	log    *silog.Logger
	opts   *Options

	tmpl *forge.ChangeTemplate
}

func newBranchSubmitForm(
	ctx context.Context,
	svc Service,
	repo GitRepository,
	remoteRepo forge.Repository,
	log *silog.Logger,
	opts *Options,
) *branchSubmitForm {
	return &branchSubmitForm{
		ctx:    ctx,
		svc:    svc,
		log:    log,
		repo:   repo,
		remote: remoteRepo,
		opts:   opts,
	}
}

func (f *branchSubmitForm) titleField(title *string) ui.Field {
	return ui.NewInput().
		WithValue(title).
		WithTitle("Title").
		WithDescription("Short summary of the change").
		WithValidate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("title cannot be blank")
			}
			return nil
		})
}

func (f *branchSubmitForm) titleFieldWithCommits(title *string, commits []git.CommitMessage) ui.Field {
	input := ui.NewInput().
		WithValue(title).
		WithTitle("Title").
		WithDescription("Short summary of the change").
		WithValidate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("title cannot be blank")
			}
			return nil
		})

	if len(commits) > 1 {
		// Extract commit subjects for options (oldest to newest)
		options := make([]string, len(commits))
		for i := len(commits) - 1; i >= 0; i-- {
			options[len(commits)-1-i] = commits[i].Subject
		}
		input = input.WithOptions(options)
	}

	return input
}

func (f *branchSubmitForm) templateField(changeTemplatesCh <-chan []*forge.ChangeTemplate) ui.Field {
	return ui.Defer(func() ui.Field {
		templates := <-changeTemplatesCh
		switch len(templates) {
		case 0:
			return nil

		case 1:
			f.tmpl = templates[0]
			return nil

		default:
			// Check if there's a default template configured
			if f.opts != nil && f.opts.Template != "" {
				wantTemplate := f.opts.Template
				for _, tmpl := range templates {
					if tmpl.Filename == wantTemplate {
						f.tmpl = tmpl
						return nil
					}
				}

				f.log.Warnf("Template %q not found", wantTemplate)
			}

			opts := make([]ui.SelectOption[*forge.ChangeTemplate], len(templates))
			for i, tmpl := range templates {
				opts[i] = ui.SelectOption[*forge.ChangeTemplate]{
					Label: tmpl.Filename,
					Value: tmpl,
				}
			}

			return ui.NewSelect[*forge.ChangeTemplate]().
				WithValue(&f.tmpl).
				WithOptions(opts...).
				WithTitle("Template").
				WithDescription("Choose a template for the change body")
		}
	})
}

func (f *branchSubmitForm) bodyField(body *string) ui.Field {
	editor := ui.Editor{
		Command: gitEditor(f.ctx, f.repo),
		Ext:     "md",
	}

	return ui.Defer(func() ui.Field {
		// By this point, the template field should have already run.
		if f.tmpl != nil {
			if *body != "" {
				*body += "\n\n"
			}
			*body += f.tmpl.Body
		}

		ed := ui.NewOpenEditor(editor).
			WithValue(body).
			WithTitle("Body").
			WithDescription("Open your editor to write " +
				"a detailed description of the change")
		ed.Style.NoEditorMessage = "" +
			"Please configure a Git core.editor, " +
			"or set the EDITOR environment variable."
		return ed
	})
}

func (f *branchSubmitForm) draftField(draft *bool) ui.Field {
	return ui.NewConfirm().
		WithValue(draft).
		WithTitle("Draft").
		WithDescription("Mark the change as a draft?")
}

// gitEditor returns the editor to use
// to prompt the user to fill information.
//
// TODO: extract this somewhere
func gitEditor(ctx context.Context, repo GitRepository) string {
	gitEditor, err := repo.Var(ctx, "GIT_EDITOR")
	if err != nil {
		// 'git var GIT_EDITOR' will basically never fail,
		// but if it does, fall back to EDITOR or vi.
		return cmp.Or(os.Getenv("EDITOR"), "vi")
	}
	return gitEditor
}
