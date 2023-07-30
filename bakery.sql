CREATE SEQUENCE public.customer_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE public.buy_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE public.bread_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE public.orders_processed_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


CREATE SEQUENCE public.bread_maker_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE public.make_order_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.bread (
                              id integer DEFAULT nextval('public.bread_id_seq'::regclass) NOT NULL,
                              name character varying(255),
                              price float,
                              quantity integer,
                              description character varying(255),
                              type character varying(255),
                              status character varying(255),
                              created_at timestamp without time zone,
                              updated_at timestamp without time zone,
                              image character varying(255),
                              PRIMARY KEY (id)
);

ALTER TABLE public.bread_id_seq OWNER TO postgres;

CREATE TABLE public.bread_maker (
                                    id integer DEFAULT nextval('public.bread_maker_id_seq'::regclass) NOT NULL,
                                    name character varying(255),
                                    email character varying(255),
                                    created_at timestamp without time zone,
                                    updated_at timestamp without time zone,
                                    PRIMARY KEY (id)
);

ALTER TABLE public.bread_maker_id_seq OWNER TO postgres;

CREATE TABLE public.make_order (
                                   id integer DEFAULT nextval('public.make_order_id_seq'::regclass) NOT NULL,
                                   bread_maker_id integer NOT NULL,
                                   make_order_uuid character varying(255),
                                   PRIMARY KEY (id),
                                   FOREIGN KEY (bread_maker_id) REFERENCES public.bread_maker(id)
);

CREATE TABLE public.make_order_details (
                                           make_order_id integer NOT NULL,
                                           bread_id integer NOT NULL,
                                           quantity integer,
                                           PRIMARY KEY (make_order_id, bread_id),
                                           FOREIGN KEY (make_order_id) REFERENCES public.make_order(id),
                                           FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);


CREATE TABLE public.customer (
                                 id integer DEFAULT nextval('public.customer_id_seq'::regclass) NOT NULL,
                                 name character varying(255),
                                 email character varying(255),
                                 password character varying(255),
                                 created_at timestamp without time zone,
                                 updated_at timestamp without time zone,
                                 PRIMARY KEY (id)
);

ALTER TABLE public.customer_id_seq OWNER TO postgres;



CREATE TABLE public.buy_order (
                                  id integer DEFAULT nextval('public.buy_id_seq'::regclass) NOT NULL,
                                  customer_id integer NOT NULL,
                                  buy_order_uuid character varying(255),
                                  status character varying(255),
                                  PRIMARY KEY (id),
                                  FOREIGN KEY (customer_id) REFERENCES public.customer(id)
);

ALTER TABLE public.buy_id_seq OWNER TO postgres;

CREATE TABLE public.order_details (
                                      buy_order_id integer NOT NULL,
                                      bread_id integer NOT NULL,
                                      quantity integer,
                                      price float,
                                      PRIMARY KEY (buy_order_id, bread_id),
                                      FOREIGN KEY (buy_order_id) REFERENCES public.buy_order(id),
                                      FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);

CREATE TABLE public.orders_processed (
                                         id integer DEFAULT nextval('public.orders_processed_id_seq'::regclass) NOT NULL,
                                         customer_id integer NOT NULL,
                                         buy_order_id integer NOT NULL,
                                         created_at timestamp without time zone,
                                         updated_at timestamp without time zone,
                                         PRIMARY KEY (id),
                                         FOREIGN KEY (customer_id) REFERENCES public.customer(id),
                                         FOREIGN KEY (buy_order_id) REFERENCES public.buy_order(id)
);

ALTER TABLE public.orders_processed_id_seq OWNER TO postgres;


INSERT INTO public.customer (name, email, password, created_at, updated_at) VALUES (
    'John Doe',
    'john@doe.com',
    '123456',
    now(),
    now()
    );

INSERT INTO public.bread_maker (name, email, created_at, updated_at) VALUES (
    'Jake Maker',
    'jake@maker.com',
    now(),
    now()
    );