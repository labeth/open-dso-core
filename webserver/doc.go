// Package webserver serves the shared web client (webclient, design) and
// the HTTP API every open scope speaks. Handlers never touch hardware: they
// call a scope's staging setters and read its frame snapshots, build each
// reply under the scope's locks and write it after they are released.
// ENGMODEL-OWNER-UNIT: FU-APP-WEB
package webserver
