package storage

import (
	"database/sql"
	"encoding/json"
	"github.com/lib/pq"
	"log"
	"time"
)

// This normally isnt needed
// but in order to pass in a fake listener
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

type PostgresDataSource struct {
	listener Listener
	add      chan LogRoute
	remove   chan LogRoute
	routes   []LogRoute
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

func (pds *PostgresDataSource) processChange(n *pq.Notification) {
	if n.Channel == "drains.insert" {
		var d drainEntry
		if err := json.Unmarshal([]byte(n.Extra), &d); err != nil {
			log.Printf("Failed to unmarshal insert notification from postgres: %s\n", err.Error())
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
			log.Printf("Failed to unmarshal insert notification from postgres: %s\n", err.Error())
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
			log.Printf("Failed to unmarshal delete notification from postgres: %s\n", err.Error())
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

func CreatePostgresDataSource(db *sql.DB, listener Listener, init bool) (*PostgresDataSource, error) {

	pds := PostgresDataSource{
		listener: listener,
		add:      make(chan LogRoute, 1),
		remove:   make(chan LogRoute, 1),
		routes:   make([]LogRoute, 0),
	}

	if init {
		_, err := db.Exec(`
			create table if not exists drains (
				drain varchar(128) primary key,
				hostname text not null,
				endpoint text not null,
				created timestamptz,
				updated timestamptz
			)
		`)
		if err != nil {
			log.Printf("create table failed with %s\n", err.Error())
			return nil, err
		}
		_, err = db.Exec(`
			create or replace function notify_drains_insert()
			  returns trigger AS $$
			declare
			begin
			  perform pg_notify('drains.insert', row_to_json(NEW)::text);
			  return NEW;
			end;
			$$ language plpgsql;
		`)
		if err != nil {
			log.Printf("create notify_drains_insert failed with %s\n", err.Error())
			return nil, err
		}
		_, err = db.Exec(`
			create or replace function notify_drains_update()
			  returns trigger AS $$
			declare
			begin
			  perform pg_notify('drains.update', '{"old":' || row_to_json(OLD)::text || ',"new":' || row_to_json(NEW)::text || '}');
			  return NEW;
			end;
			$$ language plpgsql;
		`)
		if err != nil {
			log.Printf("create notify_drains_update failed with %s\n", err.Error())
			return nil, err
		}
		_, err = db.Exec(`
			create or replace function notify_drains_delete()
			  returns trigger AS $$
			declare
			begin
			  perform pg_notify('drains.delete', row_to_json(OLD)::text);
			  return OLD;
			end;
			$$ language plpgsql;
		`)
		if err != nil {
			log.Printf("create notify_drains_delete failed with %s\n", err.Error())
			return nil, err
		}
		_, err = db.Exec(`
			do
			$do$
			begin
				if not exists (select 1 from pg_trigger where not tgisinternal and tgname='notify_drains_insert') then
					create trigger notify_drains_insert
						after insert on drains
						for each row
							execute procedure notify_drains_insert();
				end if;
			end
			$do$
		`)
		if err != nil {
			log.Printf("create trigger notify_drains_insert failed with %s\n", err.Error())
			return nil, err
		}
		_, err = db.Exec(`
			do
			$do$
			begin
				if not exists (select 1 from pg_trigger where not tgisinternal and tgname='notify_drains_update') then
					create trigger notify_drains_update
						after update on drains
						for each row
							execute procedure notify_drains_update();
				end if;
			end
			$do$
		`)
		if err != nil {
			log.Printf("create trigger notify_drains_update failed with %s\n", err.Error())
			return nil, err
		}
		_, err = db.Exec(`
			do
			$do$
			begin
				if not exists (select 1 from pg_trigger where not tgisinternal and tgname='notify_drains_delete') then
					create trigger notify_drains_delete
						after delete on drains
						for each row
							execute procedure notify_drains_delete();
				end if;
			end
			$do$
		`)
		if err != nil {
			log.Printf("create trigger notify_drains_delete failed with %s\n", err.Error())
			return nil, err
		}

		rows, err := db.Query("select drain, hostname, endpoint from drains")
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var endpoint string
			var hostname string
			if err := rows.Scan(&endpoint, &hostname); err != nil {
				return nil, err
			}
			pds.add <- LogRoute{
				Endpoint: endpoint,
				Hostname: hostname,
				Tag:      "",
			}
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
