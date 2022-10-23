package main

import (
	"bytes"
	"errors"
	"log"
	"time"

	"image"
	_ "image/jpeg"
	_ "image/png"

	"github.com/fhs/gompd/v2/mpd"
)

// TODO: Add log.Logger
func watchMPD(net string, addr string, passwd string, run func(attrs mpd.Attrs, img image.Image) (bool, error)) []error {
	errs := []error{}

	client, err := mpd.DialAuthenticated(net, addr, passwd)
	if err != nil {
		return []error{err}
	}

	if err != nil {
		return []error{err}
	}
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Print("Pinging MPD...")
				client.Ping()
			}
		}
	}()
	defer ticker.Stop()
	defer func() {
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}()

	watcher, err := mpd.NewWatcher(net, addr, passwd, "player")
	if err != nil {
		return []error{err}
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			errs = append(errs, err)
		}
	}()

	running := true

	for running {
		select {
		case subsystem := <-watcher.Event:
			if subsystem != "player" {
				return []error{errors.New("unexpected subsystem")}
			}

			attrs, err := client.CurrentSong()
			if err != nil {
				return []error{err}
			}

			var img image.Image

			if uri, ok := attrs["file"]; ok {
				data, err := client.AlbumArt(uri)
				if err == nil {
					img, _, err = image.Decode(bytes.NewReader(data))
					if err != nil {
						return []error{err}
					}
				}
			}

			running, err = run(attrs, img)
			if err != nil {
				return []error{err}
			}
		case err := <-watcher.Error:
			return []error{err}
		}
	}

	return errs
}
