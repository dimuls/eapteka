create extension pg_trgm;

drop index substance_name_idx;
drop index product_name_idx;

create index product_name_idx on product using gin (name gin_trgm_ops);
create index substance_name_idx on substance using gin (name gin_trgm_ops);