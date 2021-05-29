# eapteka

Бэкенд сервиса разработанного по заданию на хакатоне [Eapteka](https://eaptekahack.ru).

## Сборка и деплой проекта

Для сборки и деплоя проекта необходимы `docker` и `docker-compose`.

Инициализируйте git-сабмодуль с фронтендом проекта. Для этого введите команд 
в корне проекта:
```bash
git submodule init
git submodule update
```

Далее для запуска сборки и деплоя введите команду в корне проекта:
```bash
docker-compose up -d --build 
```

Проект должен быть доступен по адресу [http://127.0.0.1:10000](http://127.0.0.1:10000).

Для загрузки тестовых данных в базу данных введит команду:
```bash
docker exec eapteka /usr/bin/eapteka-data-loader
```

## Схема базы данных

![Схема базы данных](https://github.com/dimuls/eapteka/blob/master/db-scheme.png)

## Папки и go-пакеты сервиса

### [cmd/eapteka](https://github.com/dimuls/eapteka/tree/master/cmd/eapteka)

Основной код сервиса. Инициализурет соединенис базой данных, выыполняет миграцию
запускает веб-сервер.

### [cmd/eapteka-data-loader](https://github.com/dimuls/eapteka/tree/master/cmd/eapteka-data-loader)

Загрузчик тестовых данных, которые будут отображаться в прототипе фронтенда
сервиса.

### [data](https://github.com/dimuls/eapteka/tree/master/data)

Go-пакет с данными, которые встраивается в загрузчик тестовых `eapteka-data-loader`
данных при его компиляции.

### [ent](https://github.com/dimuls/eapteka/tree/master/ent)

Go-пакет с сущностями сервиса. Содержит Go-структруы, которые используется при
обмене данными с фронтендом и БД.

### [filesystem](https://github.com/dimuls/eapteka/tree/master/filesystem)

Go-пакет, которые содержит немного модифицированный [middleware](https://github.com/gofiber/fiber/tree/master/middleware/filesystem)
для веб-сервера.

### [migrations](https://github.com/dimuls/eapteka/tree/master/migrations)

Go-пакет с миграциями базы данных, которые встраиваются в основный исполняемый 
файл сервиса для инициализации схемы БД.

### [pics](https://github.com/dimuls/eapteka/tree/master/pics)

Go-пакет с картинками продукции из тестовых данных, которые встраивается в
основной сервис.

### [ui](https://github.com/dimuls/eapteka/tree/master/ui)

Git сабмодуль, который содержит [фронтенд сервиса](https://github.com/JI0PATA/eapteka).
Так-же является Go-пакетом и встраивается в основной сервис.