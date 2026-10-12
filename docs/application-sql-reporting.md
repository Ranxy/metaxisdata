# Reporting application SQL

How an application sends the SQL it actually ran, so that this deployment can show which
applications read and write which tables.

An application reports **parameterized** statements: the SQL text with every literal replaced by a
question mark. The server replaces any literal it still finds, so a mistake cannot leak a value —
but the contract is that you do the replacement, because only the application knows which values
are sensitive.

## 1. Register the application and issue a key

An administrator does this once per application, in **Settings → Applications → New application**. Creating an
application issues its first API key; the plaintext is shown once and cannot be retrieved again.
Issue another from **Keys** to rotate one, and revoke the old one afterwards.

A key belongs to exactly one application. Reports are attributed to the key, never to anything in
the request body, so a leaked key can only impersonate its own application — revoke it and issue a
new one.

The key looks like `asql_...` and is sent as a bearer token:

```
Authorization: Bearer asql_<your key>
```

## 2. Send a report

```
POST /api/v1/applicationSql/report
Authorization: Bearer asql_<your key>
Content-Type: application/json
```

```json
{
  "instance": "instances/inst-1",
  "database": "shop",
  "schema": "public",
  "reportId": "2026-01-14T09/stmt-batch",
  "statements": [
    {
      "sql": "INSERT INTO orders (id, total, status) VALUES (?, ?, ?)",
      "count": 128,
      "firstSeenAt": "2026-01-14T08:00:00Z",
      "lastSeenAt": "2026-01-14T09:00:00Z"
    },
    {
      "sql": "SELECT id, total FROM orders WHERE status = ? AND total > ?"
    }
  ]
}
```

| Field | Required | Meaning |
| --- | --- | --- |
| `instance` | yes | `instances/{id}`. The instance the application connects to. It must already be registered in this deployment. |
| `database` | yes | The database the statements run against. |
| `schema` | for PostgreSQL | The schema unqualified table names resolve against. Omit it for the MySQL family. |
| `reportId` | no | An opaque string, unique per report. See retries below. |
| `statements[].sql` | yes | One statement, literals replaced by `?`. |
| `statements[].count` | no | How many times it ran since the last report. Defaults to 1. |
| `statements[].firstSeenAt`, `lastSeenAt` | no | RFC 3339 timestamps. Default to the time the server received the report. |

Rules worth knowing:

- **One `?` per literal, in every dialect.** PostgreSQL applications send `?` too; the server
  translates as needed. `$1` is also accepted and normalized.
- **A placeholder may not appear inside a string literal or an identifier.** The server refuses a
  statement it cannot tokenize rather than guessing.
- **Send one statement per entry.** If an entry contains a script, the server splits it and each
  statement becomes its own row, with the same `count`.
- **Unknown JSON fields are rejected**, so a misspelled field is an error rather than a silently
  ignored value. The body is exactly one JSON object; a second one after it is rejected too.
- **`count` is at least 1.** Omit it for a statement that ran once. `0` and negative values are
  rejected with the body rather than read as "did not run".
- Limits: 8 MiB per request, at most 1000 statements.

## 3. Read the response

`200 OK` reports each entry's outcome. One bad statement does not lose its neighbours.

```json
{
  "application": "applications/aB3xK9mQpL2v",
  "accepted": 2,
  "rejected": 0,
  "results": [
    { "index": 0, "status": "ACCEPTED", "guid": "inst-1;shop;public;__application_sql__/kR3f9Q" },
    { "index": 1, "status": "ACCEPTED", "guid": "inst-1;shop;public;__application_sql__/Zu8NbQ" }
  ]
}
```

`accepted` and `rejected` count **entries**, so the counts add up to `results`. The per-key counters
on the Applications page count the **statements** inside those entries, so a report whose entry
carried a script contributes more than one there.

A refused entry carries a reason:

| `reason` | Meaning |
| --- | --- |
| `PARSE_ERROR` | The statement could not be tokenized to its end, or this deployment's SQL analyzer cannot parse it at all. Usually a dialect the instance does not match or an escape the dialect does not accept. An unterminated string is refused in PostgreSQL and StarRocks; the MySQL family's lexer reads one as a single literal token, so there it is stored as `?` instead. |
| `EMPTY_STATEMENT` | The entry's `sql` was empty. |
| `STORE_FAILED` | The server failed to store it. Retry the report. |

Whole-request failures use HTTP status codes: `401` for a missing, unknown, revoked or stopped key;
`400` for a malformed body, a malformed instance name, more than 1000 statements, or an instance
whose engine has no SQL analyzer; `404` for an unknown instance; `413` for a body over 8 MiB.

A refused statement is never stored, and the server logs a warning naming the application, the
engine and the reason. The warning names the statement only in its sanitized form; when the server
could not sanitize it, the line carries no statement text at all.

## 4. Retries

Reports are counted, so a retry that is processed twice reports everything twice. An **entry**
(one element of `statements`) is the unit that is stored: either every statement in it lands or none
does, so a refusal never leaves a statement counted behind it.

- **Send a `reportId`.** Repeating the `reportId` of a report that landed **whole** is answered with
  `{"duplicate": true}` and changes nothing. This covers a client that retries a request it did not
  see the answer to, which is the common case.
- **Resend only the entries that were refused.** A report that had any refusal is *not* remembered,
  so sending it again is processed again — and its accepted entries would be counted twice. Take the
  `index` of every `REJECTED` result, fix those entries, and send them as a new report with a new
  `reportId`.
- **Advance your cursor only after a whole report landed**, meaning `rejected` is `0`: either every
  entry was accepted, or the ones that were not have been resent successfully.
- A retry with no `reportId`, or with one that is not the last report that landed whole, is counted
  again. The frequencies are metadata rather than billing, so the count may drift slightly.

## 5. What you get

Every statement appears under **Application SQL**, filterable by instance, application, kind and
free text, with how often it ran and when it was first and last seen. Its detail view shows the
stored text exactly as the server kept it — literals replaced — and which applications reported it.

If the statement writes to a table, it also appears on the **Lineage** canvas as a node of its own,
connected to the tables it reads and writes, drawn in the application-SQL colour. A statement that
only reads appears with an incoming edge and nothing outgoing; a statement that neither reads a
column nor names a write target (`SELECT 1`, `SET`) is stored but not drawn.

Deleting an application removes every statement no other application has reported, together with
its lineage. Stopping one keeps everything and simply refuses its keys.

## 6. Privacy

The only form of a statement that reaches the database is the server's own sanitized output: every
literal replaced, comments removed. The text you send is never stored, so a value that slips
through your own replacement is replaced again before it is written.

What is stored is therefore the *shape* of the query, not its data. Reports are attributed to an
application, and per-request audit entries are deliberately not written — a reporting key carries
counters instead — because the audit ledger is kept forever and machine traffic would grow it
without bound.
