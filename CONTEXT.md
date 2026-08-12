# LoomTable Context

LoomTable is a self-hosted structured-data workspace whose primary client is an Obsidian plugin. It presents tables, records, fields, and views as one coherent product while keeping the server as the source of truth.

## Language

### Product structure

**Workspace**:
A personal or team-owned space that contains Bases and their data.
_Avoid_: account, project space

**Base**:
A logical multi-dimensional data container that groups related Tables.
_Avoid_: database, data source

**Table**:
A collection of Records described by Fields.
_Avoid_: sheet, page

**View**:
A named query and presentation of one Table; a View does not own a second copy of the Records.
_Avoid_: page, data copy

**Grid View**:
A spreadsheet-like View for browsing and editing Records.
_Avoid_: spreadsheet, table page

**Map View**:
A geographic View that places Records with coordinate-bearing Location values on a map.
_Avoid_: GIS page, map table

**Map Point**:
The representation of one located Record in a Map View.
_Avoid_: marker record, GeoPoint

**Map Cluster**:
A map-scale summary of multiple Map Points that are too dense to present individually.
_Avoid_: grouped Record, aggregate Record

**Default Camera**:
The saved initial center and zoom of a Map View; it is distinct from a client's temporary browsing position.
_Avoid_: current viewport, last pan position

**Map Viewport**:
The temporary geographic extent currently visible in one Map View instance; it is queried with the current zoom and pixel dimensions and is not saved as View configuration.
_Avoid_: Default Camera, Map View config

**Unlocated Record**:
A Record matched by a Map View whose selected Location value is missing or is not a valid WGS 84 coordinate.
_Avoid_: hidden Record, unrenderable Location

**Unrenderable Location**:
A valid WGS 84 Location coordinate outside the latitude range that P0's EPSG:3857 map can render; the original Location remains stored.
_Avoid_: invalid Location, Unlocated Record

**Tile Provider**:
An external service that supplies the visual map tiles used by a Map View; it is independent of LoomTable Server and the source of a Location value.
_Avoid_: map source, LoomTable map server

**Tile Provider Profile**:
A named client-side configuration for using one Tile Provider without embedding its credential in LoomTable data.
_Avoid_: Map View config, tile URL field

### Data concepts

**Field**:
A named definition of one kind of value that a Table can hold for each Record.
_Avoid_: property, column configuration

**Field Type**:
The semantic kind of value accepted by a Field, including its validation, editing, display, and query behavior.
_Avoid_: data format, widget type

**Record**:
An independently identifiable item of data in a Table.
_Avoid_: row, item line

**Cell**:
The value of one Field for one Record.
_Avoid_: field value slot

**Unset Cell**:
A Field-and-Record intersection for which no value has been supplied; it is distinct from an explicit null or a type-specific empty value.
_Avoid_: null Cell, empty string

**Primary Field**:
The user-facing Field used to identify a Record in lists, selectors, and summaries.
_Avoid_: ID field, title column

**Location**:
A place value that may contain a label, address, and geographic coordinates.
_Avoid_: place text, GeoPoint field

**GeoPoint**:
The coordinate value inside a Location, expressed as latitude and longitude.
_Avoid_: Location field

**Region**:
A standardized administrative area value selected from a versioned geographic hierarchy, such as country, province, city, or district.
_Avoid_: Location field, free-form area text

**DateTime**:
A date and time value representing an instant, stored with an unambiguous time basis.
_Avoid_: Date, localized display string

**Time**:
A time-of-day value without a calendar date.
_Avoid_: DateTime, duration

**GeoWithin**:
A spatial condition that matches Location values whose coordinates lie inside a specified geographic shape.
_Avoid_: map selection only, region filter

**Attachment**:
A reference to file content associated with a Cell or Record.
_Avoid_: file path, binary field

**Managed Attachment**:
An Attachment whose file content is managed by LoomTable storage.
_Avoid_: remote file, server file

**Vault Attachment**:
An Attachment that refers to a file in an Obsidian Vault.
_Avoid_: local path attachment

**Relation**:
A Field that references one or more Records in another Table within the same Base.
_Avoid_: foreign key field, link text

**Computed Field**:
A read-only Field whose value is derived from other data.
_Avoid_: formula column, calculated cell

### Change and deployment concepts

**Mutation**:
A requested change to one or more Records or schema objects.
_Avoid_: write event, database update

**Revision**:
The version of a Record used to determine whether a Mutation is based on current data.
_Avoid_: timestamp, sync version

**Change**:
A durable fact that a Mutation changed LoomTable data.
_Avoid_: request, operation

**Change Cursor**:
A position from which a client can request later Changes.
_Avoid_: page number, sync token

**Conflict**:
A rejected Mutation whose expected Revision is older than the current Revision.
_Avoid_: merge error, overwrite warning

**Recycle State**:
The retained state of a soft-deleted LoomTable object that can still be discovered and restored.
_Avoid_: hard deletion, trash copy

**Actor**:
A stable identity to which authenticated LoomTable changes are attributed; it is not itself a login credential.
_Avoid_: Token, session, user account

**Access Token**:
A named secret credential that authorizes requests as an Actor. One Actor may have multiple independently revocable Access Tokens without changing its identity.
_Avoid_: Actor ID, user identity, password

**Personal**:
A single-user deployment profile that may be local or remote and does not include real-time collaboration.
_Avoid_: local-only mode

**Team**:
A future deployment profile with multiple users, permissions, real-time collaboration, and background coordination.
_Avoid_: shared Personal

**Vault**:
The Obsidian file space used by the Plugin, including its notes and local files.
_Avoid_: local database