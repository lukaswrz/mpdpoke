package main

import (
	"bytes"
	"log"
	"time"

	"image"
	_ "image/jpeg"
	_ "image/png"

	"github.com/fhs/gompd/v2/mpd"
)

func watchMPD(net string, addr string, passwd string, interval time.Duration, run func(attrs, status mpd.Attrs, img image.Image) (bool, error)) []error {
	errs := []error{}

	client, err := mpd.DialAuthenticated(net, addr, passwd)
	if err != nil {
		return []error{err}
	}

	if err != nil {
		return []error{err}
	}
	ticker := time.NewTicker(interval * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Print("Pinging MPD")
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
				continue
			}

			log.Print("Processing MPD event")

			attrs, err := client.CurrentSong()
			if err != nil {
				log.Printf("Unable to get the current song: ", err)
				continue
			}

			status, err := client.Status()
			if err != nil {
				log.Printf("Unable to obtain the current MPD status: %s", err)
				continue
			}

			var img image.Image

			if uri, ok := attrs["file"]; ok {
				data, err := client.AlbumArt(uri)
				if err != nil {
					log.Printf("Cannot retrieve album art: %s", err);
				} else {
					img, _, err = image.Decode(bytes.NewReader(data))
					if err != nil {
						log.Printf("Cannot decode the image sent by MPD: ", err)
					}
				}
			}

			running, err = run(attrs, status, img)
			if err != nil {
				return []error{err}
			}
		case err := <-watcher.Error:
			return []error{err}
		}
	}

	return errs
}
