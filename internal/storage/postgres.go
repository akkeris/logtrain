package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/lib/pq"
	"time"
)

// This normally isnt needed but in order to pass in a fake listener
// for testing we need this.
type Listener interface {
	Close() error
	Listen(string) error
	NotificationChannel() <-chan *pq.Notification
	Ping() error
}

type drainEntry struct {
	Drain    string `json:"drain"`
	Hostname string `json:"hostname"`
	Endpoint string `json:"endpoint"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
}

type drainEntryUpdate struct {
	Old drainEntry `json:"old"`
	New drainEntry `json:"new"`
}

var creationScript = `
do
$do$
begin
	create extension if not exists "uuid-ossp";

	create table if not exists drains (
		drain uuid primary key default uuid_generate_v4(),
		hostname text not null,
		endpoint text not null,
		created timestamptz,
		updated timestamptz
	);

	create or replace function notify_drains_insert()
	  returns trigger AS $$
	declare
	begin
	  perform pg_notify('drains.insert', row_to_json(NEW)::text);
	  return NEW;
	end;
	$$ language plpgsql;

	create or replace function notify_drains_update()
	  returns trigger AS $$
	declare
	begin
	  perform pg_notify('drains.update', '{"old":' || row_to_json(OLD)::text || ',"new":' || row_to_json(NEW)::text || '}');
	  return NEW;
	end;
	$$ language plpgsql;

	create or replace function notify_drains_delete()
	  returns trigger AS $$
	declare
	begin
	  perform pg_notify('drains.delete', row_to_json(OLD)::text);
	  return OLD;
	end;
	$$ language plpgsql;

	if not exists (select 1 from pg_trigger where not tgisinternal and tgname='notify_drains_insert') then
		create trigger notify_drains_insert
			after insert on drains
			for each row
				execute procedure notify_drains_insert();
	end if;

	if not exists (select 1 from pg_trigger where not tgisinternal and tgname='notify_drains_update') then
		create trigger notify_drains_update
			after update on drains
			for each row
				execute procedure notify_drains_update();
	end if;

	if not exists (select 1 from pg_trigger where not tgisinternal and tgname='notify_drains_delete') then
		create trigger notify_drains_delete
			after delete on drains
			for each row
				execute procedure notify_drains_delete();
	end if;

end
$do$
`

type PostgresDataSource struct {
	listener Listener
	add      chan LogRoute
	remove   chan LogRoute
	routes   []LogRoute
	db       *sql.DB
	closed   bool
}

func (pds *PostgresDataSource) AddRoute() chan LogRoute {
	return pds.add
}

func (pds *PostgresDataSource) RemoveRoute() chan LogRoute {
	return pds.remove
}

func (pds *PostgresDataSource) GetAllRoutes() ([]LogRoute, error) {
	return pds.routes, nil
}

// EmitNewRoute always returns an error as this datasource is not currently writable.
func (pds *PostgresDataSource) EmitNewRoute(route LogRoute) error {
	if pds.closed {
		return errors.New("datasource is closed")
	}
	if _, err := pds.db.Exec("insert into drains (hostname, endpoint, tag) values ($1, $2, $3)", route.Hostname, route.Endpoint, route.Tag); err != nil {
		return err
	}
	return nil
}

// EmitRemoveRoute always returns an error as this datasource is not currently writable.
func (pds *PostgresDataSource) EmitRemoveRoute(route LogRoute) error {
	if pds.closed {
		return errors.New("datasource is closed")
	}
	if _, err := pds.db.Exec("delete drains where hostname = $1 and endpoint = $2 and tag = $3", route.Hostname, route.Endpoint, route.Tag); err != nil {
		return err
	}
	return nil
}

// Writable returns false always as this datasource is not writable.
func (pds *PostgresDataSource) Writable() bool {
	return true
}

// Close closes the postgres datasource.
func (pds *PostgresDataSource) Close() error {
	if pds.closed {
		return errors.New("this datasource is already closed")
	}
	pds.closed = true
	close(pds.add)
	close(pds.remove)
	pds.db.Close()
	return nil
}

func (pds *PostgresDataSource) processChange(n *pq.Notification) {
	if n.Channel == "drains.insert" {
		var d drainEntry
		if err := json.Unmarshal([]byte(n.Extra), &d); err != nil {
			debug.Errorf("Failed to unmarshal insert notification from postgres: %s\n", err.Error())
		} else {
			pds.add <- LogRoute{
				Endpoint: d.Endpoint,
				Hostname: d.Hostname,
				Tag:      "",
			}
		}
	} else if n.Channel == "drains.update" {
		var d drainEntryUpdate
		if err := json.Unmarshal([]byte(n.Extra), &d); err != nil {
			debug.Errorf("Failed to unmarshal insert notification from postgres: %s\n", err.Error())
		} else {
			if d.Old.Endpoint != d.New.Endpoint || d.Old.Hostname != d.New.Hostname {
				pds.remove <- LogRoute{
					Endpoint: d.Old.Endpoint,
					Hostname: d.Old.Hostname,
					Tag:      "",
				}
				pds.add <- LogRoute{
					Endpoint: d.New.Endpoint,
					Hostname: d.New.Hostname,
					Tag:      "",
				}
			}
		}
	} else if n.Channel == "drains.delete" {
		var d drainEntry
		if err := json.Unmarshal([]byte(n.Extra), &d); err != nil {
			debug.Errorf("Failed to unmarshal delete notification from postgres: %s\n", err.Error())
		} else {
			pds.remove <- LogRoute{
				Endpoint: d.Endpoint,
				Hostname: d.Hostname,
				Tag:      "",
			}
		}
	}
}

func (pds *PostgresDataSource) listenForChanges() {
	for {
		select {
		case n := <-pds.listener.NotificationChannel():
			pds.processChange(n)
		case <-time.After(time.Minute):
			pds.listener.Ping()
		}
	}
}

// Dial connects the data source
func (pds *PostgresDataSource) Dial() error {
	return nil
}

func CreatePostgresDataSource(db *sql.DB, listener Listener, init bool) (*PostgresDataSource, error) {
	pds := PostgresDataSource{
		listener: listener,
		add:      make(chan LogRoute, 1),
		remove:   make(chan LogRoute, 1),
		routes:   make([]LogRoute, 0),
		db:       db,
		closed:   false,
	}

	if init {
		if _, err := db.Exec(creationScript); err != nil {
			return nil, err
		}
		rows, err := db.Query("select drain, hostname, endpoint from drains")
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var logroute LogRoute
			if err := rows.Scan(&logroute.Endpoint, &logroute.Hostname); err != nil {
				return nil, err
			}
			pds.add <- logroute
		}
	}

	if err := pds.listener.Listen("drains.insert"); err != nil {
		return nil, err
	}
	if err := pds.listener.Listen("drains.update"); err != nil {
		return nil, err
	}
	if err := pds.listener.Listen("drains.delete"); err != nil {
		return nil, err
	}

	go pds.listenForChanges()

	return &pds, nil
}

// CreatePostgresDataSourceWithURL creates a postgres datasource from a database url.
func CreatePostgresDataSourceWithURL(databaseURL string) (*PostgresDataSource, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	listener := pq.NewListener(databaseURL, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			debug.Fatalf("[postgres] Error in listener to postgres: %s\n", err.Error())
		}
	})
	pds, err := CreatePostgresDataSource(db, listener, true)
	if err != nil {
		return nil, err
	}
	return pds, nil
}
