create table notifier (
    id bigserial primary key,
    product_id bigint not null,
    schedule varchar(10)[] not null
)