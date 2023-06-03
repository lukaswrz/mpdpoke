package main

import (
	"errors"
	"html"
	"image"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/esiqveland/notify"
	"github.com/fhs/gompd/v2/mpd"
	"github.com/godbus/dbus/v5"
	"github.com/urfave/cli/v2"
)

type mpdconfig struct {
	Address  string        `toml:"address"`
	Password string        `toml:"password"`
	Interval time.Duration `toml:"interval"`
}

type config struct {
	PlayIcon            string        `toml:"play_icon"`
	PauseIcon           string        `toml:"pause_icon"`
	NotificationTimeout time.Duration `toml:"notification_timeout"`
	MPD                 mpdconfig     `toml:"mpd"`
}

type imgdata struct {
	Width         int
	Height        int
	RowStride     int
	HasAlpha      bool
	BitsPerSample int
	Samples       int
	Image         []byte
}

func readConfig(path string, paths []string, unmarshal func(data []byte, v interface{}) error, v interface{}) error {
	var err error

	if path == "" {
		for _, p := range paths {
			_, err = os.Stat(p)
			if err != nil {
				continue
			}

			path = p
			break
		}

		if path == "" {
			return errors.New("unable to locate configuration file")
		}
	} else {
		_, err = os.Stat(path)
		if err != nil {
			return err
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	err = unmarshal(content, v)
	if err != nil {
		return err
	}

	return nil
}

func getConfigHome() (string, error) {
	if ch, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		return ch, nil
	}

	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return path.Join(h, ".config"), nil
}

func determineNetwork(addr string) (string, error) {
	if len(addr) == 0 {
		return "", errors.New("invalid address")
	}

	if addr[0] == '/' {
		return "unix", nil
	} else {
		return "tcp", nil
	}
}

func main() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
	log.SetPrefix("mpdpoke: ")

	c := config{
		PlayIcon:            "\u25B6",
		PauseIcon:           "\u23F8",
		NotificationTimeout: 5,
		MPD: mpdconfig{
			Address:  "127.0.0.1:6600",
			Password: "",
			Interval: 60,
		},
	}

	paths := []string{"mpdpoke.toml"}
	if ch, err := getConfigHome(); err == nil {
		paths = append(paths, path.Join(ch, "mpdpoke/mpdpoke.toml"))
	}
	paths = append(paths, "/etc/mpdpoke/mpdpoke.toml")

	var cf string

	app := &cli.App{
		Name:  "mpdpoke",
		Usage: "notify when tracks are played by mpd",
		Action: func(ctx *cli.Context) error {
			return nil
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Usage:       "configuration file",
				Destination: &cf,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("Argument error: %s", err.Error())
	}

	err := readConfig(
		cf,
		paths,
		toml.Unmarshal,
		&c,
	)
	if err != nil {
		log.Fatalf("Error while attempting to read the configuration file: %s", err.Error())
	}

	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		log.Fatalf("Could not connect to the session bus: %s", err.Error())
	}
	defer conn.Close()

	if err = conn.Auth(nil); err != nil {
		log.Fatalf("Could not authenticate the session: %s", err.Error())
	}

	if err = conn.Hello(); err != nil {
		log.Fatalf("Could not send initial DBus call: %s", err.Error())
	}

	var createdID uint32
	sent := false

	net, err := determineNetwork(c.MPD.Address)
	if err != nil {
		log.Fatalf("Error while determining network: %s", err.Error())
	}

	errs := watchMPD(net, c.MPD.Address, c.MPD.Password, c.MPD.Interval, func(attrs, status mpd.Attrs, img image.Image) (bool, error) {
		if _, ok := attrs["Title"]; !ok {
			return true, nil
		}

		n := notify.Notification{
			AppName:       "mpdpoke",
			AppIcon:       "audio-x-generic",
			Summary:       "-",
			Hints:         map[string]dbus.Variant{},
			ExpireTimeout: time.Second * c.NotificationTimeout,
		}

		if title, ok := attrs["Title"]; ok {
			prefix := ""

			if state, ok := status["state"]; ok {
				switch state {
				case "play":
					prefix = c.PlayIcon + " "
				case "pause":
					prefix = c.PauseIcon + " "
				}
			}

			n.Summary = prefix + title
		}

		body := []string{}

		if artist, ok := attrs["Artist"]; ok {
			body = append(body, html.EscapeString(artist))
		}

		if album, ok := attrs["Album"]; ok {
			body = append(body, "<i>"+html.EscapeString(album)+"</i>")
		}

		n.Body = strings.Join(body, "\n")

		p, ok := img.(*image.RGBA)
		if ok {
			r := p.Bounds()
			n.Hints["image-data"] = dbus.MakeVariant(imgdata{
				r.Max.X,
				r.Max.Y,
				p.Stride,
				true,
				8,
				4,
				p.Pix,
			})
		}

		if sent {
			n.ReplacesID = createdID
		}

		createdID, err = notify.SendNotification(conn, n)
		if err != nil {
			return false, err
		}
		sent = true

		return true, nil
	})
	if errs != nil && len(errs) != 0 {
		for _, err := range errs {
			log.Printf("Error while watching: %s", err))
		}
		os.Exit(1)
	}
}
