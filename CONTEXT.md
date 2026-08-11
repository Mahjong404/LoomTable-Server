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

**Primary Field**:
The user-facing Field used to identify a Record in lists, selectors, and summaries.
_Avoid_: ID field, title column

**Location**:
A place value that may contain a label, address, and geographic coordinates.
_Avoid_: place text, GeoPoint field

**GeoPoint**:
The coordinate value inside a Location, expressed as latitude and longitude.
_Avoid_: Location field

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

**Personal**:
A single-user deployment profile that may be local or remote and does not include real-time collaboration.
_Avoid_: local-only mode

**Team**:
A future deployment profile with multiple users, permissions, real-time collaboration, and background coordination.
_Avoid_: shared Personal

**Vault**:
The Obsidian file space used by the Plugin, including its notes and local files.
_Avoid_: local database

