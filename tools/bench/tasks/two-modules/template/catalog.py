"""Acme's catalog: one product."""

from dataclasses import dataclass


@dataclass(frozen=True)
class Product:
    sku: str
    name: str
    price_cents: int
    tags: tuple = ()
