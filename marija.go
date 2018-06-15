/*
* Honeytrap
* Copyright (C) 2016-2017 DutchSec (https://dutchsec.com/)
*
* This program is free software; you can redistribute it and/or modify it under
* the terms of the GNU Affero General Public License version 3 as published by the
* Free Software Foundation.
*
* This program is distributed in the hope that it will be useful, but WITHOUT
* ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
* FOR A PARTICULAR PURPOSE.  See the GNU Affero General Public License for more
* details.
*
* You should have received a copy of the GNU Affero General Public License
* version 3 along with this program in the file "LICENSE".  If not, see
* <http://www.gnu.org/licenses/agpl-3.0.txt>.
*
* See https://honeytrap.io/ for more details. All requests should be sent to
* licensing@honeytrap.io
*
* The interactive user interfaces in modified source and object code versions
* of this program must display Appropriate Legal Notices, as required under
* Section 5 of the GNU Affero General Public License version 3.
*
* In accordance with Section 7(b) of the GNU Affero General Public License version 3,
* these Appropriate Legal Notices must retain the display of the "Powered by
* Honeytrap" logo and retain the original copyright notice. If the display of the
* logo is not reasonably feasible for technical reasons, the Appropriate Legal Notices
* must display the words "Powered by Honeytrap" and retain the original copyright notice.
 */
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"time"

	"io"
)

type Config struct {
	URL string `toml:"url"`

	Insecure bool `toml:"insecure"`
}

// MarijaClient defines a struct which provides a channel for delivery
// push messages to an elasticsearch api.
type MarijaClient struct {
	Config

	wg sync.WaitGroup

	done chan struct{}
	ch   chan map[string]interface{}
}

func WithURL(u string) func(*MarijaClient) error {
	return func(b *MarijaClient) error {
		b.URL = u
		return nil
	}
}

func New(options ...func(*MarijaClient) error) (*MarijaClient, error) {
	ch := make(chan map[string]interface{}, 100)

	c := MarijaClient{
		ch:   ch,
		wg:   sync.WaitGroup{},
		done: make(chan struct{}),
	}

	for _, optionFn := range options {
		optionFn(&c)
	}

	return &c, nil
}

func Insecure(config *tls.Config) *tls.Config {
	config.InsecureSkipVerify = true
	return config
}

func (hc *MarijaClient) Start() {
	hc.wg.Add(1)
	go hc.run()
}

func (hc *MarijaClient) Stop() {
	close(hc.ch)

	hc.wg.Wait()
}

func (hc *MarijaClient) run() {
	defer hc.wg.Done()

	tlsClientConfig := &tls.Config{}

	if hc.Insecure {
		tlsClientConfig = Insecure(tlsClientConfig)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsClientConfig,
		},
	}

	var wg sync.WaitGroup

	docs := make([]map[string]interface{}, 0)

	send := func(docs []map[string]interface{}) {
		if len(docs) == 0 {
			return
		}

		wg.Add(1)

		pr, pw := io.Pipe()
		go func() {
			defer wg.Done()

			var err error

			defer pw.CloseWithError(err)

			for _, doc := range docs {
				err = json.NewEncoder(pw).Encode(doc)
				if err != nil {
					return
				}
			}
		}()

		req, err := http.NewRequest(http.MethodPost, hc.URL, pr)
		if err != nil {
			fmt.Println("Could create new request: %s", err.Error())
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("Could not submit event to Marija: %s", err.Error())
			return
		}

		if resp.StatusCode != http.StatusOK {
			fmt.Println("Could not submit event to Marija: %d", resp.StatusCode)
			return
		}
	}

	defer func() {
		send(docs)
	}()

	for {
		select {
		case doc, ok := <-hc.ch:
			if !ok {
				return
			}

			docs = append(docs, doc)

			if len(docs) < 100 {
				//				continue
			}

			send(docs)

			docs = make([]map[string]interface{}, 0)
		case <-time.After(time.Second * 2):
			send(docs)

			docs = make([]map[string]interface{}, 0)
		}
	}
}

// Send delivers the giving push messages into the internal elastic search endpoint.
func (hc *MarijaClient) Send(message map[string]interface{}) {
	hc.ch <- message
}
