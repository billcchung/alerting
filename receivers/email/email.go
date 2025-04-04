package email

import (
	"context"
	"net/url"
	"path"

	"github.com/prometheus/alertmanager/types"

	"github.com/billcchung/alerting/images"
	"github.com/billcchung/alerting/logging"
	"github.com/billcchung/alerting/receivers"
	"github.com/billcchung/alerting/templates"
)

// Notifier is responsible for sending
// alert notifications over email.
type Notifier struct {
	*receivers.Base
	log      logging.Logger
	ns       receivers.EmailSender
	images   images.Provider
	tmpl     *templates.Template
	settings Config
}

func New(cfg Config, meta receivers.Metadata, template *templates.Template, sender receivers.EmailSender, images images.Provider, logger logging.Logger) *Notifier {
	return &Notifier{
		Base:     receivers.NewBase(meta),
		log:      logger,
		ns:       sender,
		images:   images,
		tmpl:     template,
		settings: cfg,
	}
}

// Notify sends the alert notification.
func (en *Notifier) Notify(ctx context.Context, alerts ...*types.Alert) (bool, error) {
	var tmplErr error
	tmpl, data := templates.TmplText(ctx, en.tmpl, alerts, en.log, &tmplErr)

	subject := tmpl(en.settings.Subject)
	alertPageURL := en.tmpl.ExternalURL.String()
	ruleURL := en.tmpl.ExternalURL.String()
	u, err := url.Parse(en.tmpl.ExternalURL.String())
	if err == nil {
		basePath := u.Path
		u.Path = path.Join(basePath, "/alerting/list")
		ruleURL = u.String()
		u.RawQuery = "alertState=firing&view=state"
		alertPageURL = u.String()
	} else {
		en.log.Debug("failed to parse external URL", "url", en.tmpl.ExternalURL.String(), "error", err.Error())
	}

	// Extend alerts data with images, if available.
	embeddedContents := make([]receivers.EmbeddedContent, 0)
	err = images.WithStoredImages(ctx, en.log, en.images,
		func(index int, image images.Image) error {
			if image.HasURL() {
				data.Alerts[index].ImageURL = image.URL
			} else {
				if contents, err := image.RawData(ctx); err == nil {
					data.Alerts[index].EmbeddedImage = contents.Name
					embeddedContents = append(embeddedContents, receivers.EmbeddedContent{
						Name:    contents.Name,
						Content: contents.Content,
					})
				} else {
					en.log.Warn("failed to get image file for email attachment", "alert", alerts[index].String(), "error", err)
				}
			}
			return nil
		}, alerts...)
	if err != nil {
		en.log.Warn("failed to get all images for email", "error", err)
	}

	cmd := &receivers.SendEmailSettings{
		Subject: subject,
		Data: map[string]interface{}{
			"Title":             subject,
			"Message":           tmpl(en.settings.Message),
			"Status":            data.Status,
			"Alerts":            data.Alerts,
			"GroupLabels":       data.GroupLabels,
			"CommonLabels":      data.CommonLabels,
			"CommonAnnotations": data.CommonAnnotations,
			"ExternalURL":       data.ExternalURL,
			"RuleUrl":           ruleURL,
			"AlertPageUrl":      alertPageURL,
		},
		EmbeddedContents: embeddedContents,
		To:               en.settings.Addresses,
		SingleEmail:      en.settings.SingleEmail,
		Template:         "ng_alert_notification",
	}

	if tmplErr != nil {
		en.log.Warn("failed to template email message", "error", tmplErr.Error())
	}

	if err := en.ns.SendEmail(ctx, cmd); err != nil {
		return false, err
	}

	return true, nil
}

func (en *Notifier) SendResolved() bool {
	return !en.GetDisableResolveMessage()
}
