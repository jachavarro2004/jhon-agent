# Importing a supplier's CSV

Suppliers send a CSV file, UTF-8, possibly with a byte-order mark, with a
header row and the columns `sku`, `name`, `price` and `tags`, separated by
`;` (not commas).

- **sku**: trimmed and upper-cased. A row whose sku is empty is rejected.
- **name**: trimmed, with every run of whitespace inside it made one space.
- **price**: European notation: `.` groups thousands and `,` marks the
  decimals (`1.234,56` is 1234.56; `7` is 7.00). It becomes `price_cents`. A
  price that is not such a number, has more than two decimals or is negative
  rejects the row.
- **tags**: separated by `|`; each trimmed and lower-cased; empty ones
  dropped, repeats dropped, the rest sorted. A product's tags are a tuple.
- When a sku appears in more than one row, the later row replaces the
  earlier one; a replaced row is not a rejected row.

`import_products(path)` returns `(products, rejected)`: the products and the
number of rows rejected.
