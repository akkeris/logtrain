package persistent

import (
	"database/sql"
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"net/url"
	"os"
	"strings"
	"time"
)

// Syslog creates a new syslog output
type Syslog struct {
	id       string
	endpoint string
	url      url.URL
	packets  chan syslog.Packet
	errors   chan<- error
	stop     chan struct{}
	db       *sql.DB
}

const maxLogSize int = 99990

var schemas = []string{"persistent://"}

var createStatement = `
do
$do$
begin
	create table if not exists logs (
		id varchar(128) primary key not null,
		data text not null default ''
	);
end
$do$
`
var selectStatement = `select data from logs where id = $1`
var insertStatement = `insert into logs (id, data) values ($1, '')`
var updateStatement = `update logs set data = (data || $1) where id = $2`
var sqlDriver = "postgres"

// Get gets logs by the specified key passed in
func Get(key string, db *sql.DB) (*string, error) {
	if db == nil {
		var err error
		db, err = sql.Open(sqlDriver, os.Getenv("PERSISTENT_DATABASE_URL")) // TODO: this should be passed in or better yet receive a storage object..
		if err != nil {
			return nil, err
		}
		defer db.Close()
	}

	var result string
	if err := db.QueryRow(selectStatement, key).Scan(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Test the schema to see if its an schema
func Test(endpoint string) bool {
	if os.Getenv("PERSISTENT_DATABASE_URL") == "" { // TODO: this should be passed in or better yet receive a storage object..
		return false
	}
	for _, schema := range schemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

// Create a new endpoint
func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("invalid endpoint")
	}
	db, err := sql.Open(sqlDriver, os.Getenv("PERSISTENT_DATABASE_URL")) // TODO: this should be passed in or better yet receive a storage object..
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(createStatement); err != nil {
		db.Close()
		return nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		db.Close()
		return nil, err
	}
	id := u.Host
	if id == "" {
		db.Close()
		return nil, errors.New("invalid key")
	}

	if _, err := db.Exec(insertStatement, id); err != nil {
		db.Close()
		return nil, err
	}
	return &Syslog{
		id:       id,
		endpoint: endpoint,
		url:      *u,
		packets:  make(chan syslog.Packet, 10),
		errors:   errorsCh,
		stop:     make(chan struct{}, 1),
		db:       db,
	}, nil
}

// Dial connects persistent storage
func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}

// Close closes the connection to peristent
func (log *Syslog) Close() error {
	log.db.Close()
	log.stop <- struct{}{}
	close(log.packets)
	return nil
}

// Pools returns whether the persistent end point pools connections
func (log *Syslog) Pools() bool {
	return true
}

// Packets returns a channel to send syslog packets on
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}

func (log *Syslog) loop() {
	timer := time.NewTicker(time.Second)
	var payload string
	var errors = 0
	for {
		select {
		case p, ok := <-log.packets:
			if ok {
				payload = payload + p.Generate(maxLogSize) + "\n"
			}
		case <-timer.C:
			if payload != "" {
				if _, err := log.db.Exec(updateStatement, payload, log.id); err != nil {
					debug.Errorf("[persistent] An error occured while trying to update persistent logs in database %s: %s\n", log.id, err.Error())
					errors++
					if errors > 10 {
						debug.Errorf("[persistent] An error occured while trying to update persistent logs in database %s: %s\n", log.id, err.Error())
						return
					}
				}
				payload = ""
			}
		case <-log.stop:
			return
		}
	}
}
