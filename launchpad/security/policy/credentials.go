package policy

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
	"strings"
	"time"
)

type Credential struct {
	ID, Owner, Audience, Kind         string
	Expires                           time.Time
	Schemas, Subjects, ObjectPrefixes []string
	Migration, DeleteObjects          bool
}

func ValidateCredentials(inventory []Credential, now time.Time) error {
	if len(inventory) == 0 || len(inventory) > 1024 {
		return ErrDenied
	}
	seen := map[string]bool{}
	for _, c := range inventory {
		if len(c.ID) > 128 || len(c.Owner) > 128 || len(c.Audience) > 128 || len(c.Schemas) > 32 || len(c.Subjects) > 64 || len(c.ObjectPrefixes) > 64 || c.ID == "" || seen[c.ID] || c.Owner == "" || c.Audience == "" || !c.Expires.After(now) || c.Kind == "" || c.Migration && c.Expires.Sub(now) > 15*time.Minute || c.DeleteObjects && c.Kind == "upload" {
			return ErrDenied
		}
		seen[c.ID] = true
		for _, s := range c.Schemas {
			if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(s) {
				return ErrDenied
			}
		}
		for _, subject := range c.Subjects {
			if subject == "" || len(subject) > 256 || strings.ContainsAny(subject, "*> \r\n\x00") || strings.Contains(subject, "..") {
				return ErrDenied
			}
		}
		for _, prefix := range c.ObjectPrefixes {
			if prefix == "" || len(prefix) > 512 || strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "*\\\r\n\x00") {
				return ErrDenied
			}
			for _, part := range strings.Split(prefix, "/") {
				if part == ".." || part == "." {
					return ErrDenied
				}
			}
		}
	}
	return nil
}
func ValidateDatabaseRole(ctx context.Context, pool *pgxpool.Pool, ownedSchema string) error {
	if pool == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(ownedSchema) {
		return ErrDenied
	}
	var privileged bool
	err := pool.QueryRow(ctx, `SELECT r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolbypassrls OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname=$1 AND pg_has_role(current_user,n.nspowner,'MEMBER')) FROM pg_roles r WHERE r.rolname=current_user`, ownedSchema).Scan(&privileged)
	if err != nil {
		return err
	}
	if privileged {
		return ErrDenied
	}
	var foreignWrite bool
	err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname<>$1 AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN ('r','p','v','f') AND (has_table_privilege(current_user,c.oid,'INSERT') OR has_table_privilege(current_user,c.oid,'UPDATE') OR has_table_privilege(current_user,c.oid,'DELETE') OR has_table_privilege(current_user,c.oid,'TRUNCATE')))`, ownedSchema).Scan(&foreignWrite)
	if err != nil {
		return err
	}
	if foreignWrite {
		return ErrDenied
	}
	return nil
}

func safeIdentifier(s string) bool {
	return regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(s)
}
