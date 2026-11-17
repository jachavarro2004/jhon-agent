# Exporting to the storefront

The storefront reads JSON Lines: one JSON object per line, UTF-8, every line
ending in `\n`. No products is an empty file.

- Lines are sorted by sku.
- Each object has exactly the keys `sku`, `name`, `price`, `tags`, in that
  order. `price` is a string with two decimals and a `.` (`1234.56`, `7.00`);
  `tags` is a list.
- Text is written as it is: `é` stays `é`, never `é`.
- A product whose price is 0 is a free sample and is not exported.
