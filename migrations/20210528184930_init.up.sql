create table purchase (
    id bigserial primary key,
    created_at timestamp with time zone not null default now()
);

create table substance (
    id bigserial primary key,
    name text not null
);

create index substance_name_idx on substance using gin (to_tsvector('russian', name));

create table product (
    id bigserial primary key,
    substance_id bigint not null references substance (id),
    name text not null,
    description text not null
);

create index product_name_idx on product using gin (to_tsvector('russian', name));

create table purchase_product (
    purchase_id bigint not null references purchase (id),
    product_id bigint not null references product (id),
    count integer not null
);


