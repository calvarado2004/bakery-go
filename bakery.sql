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
                              PRIMARY KEY (id),
                              CONSTRAINT bread_quantity_non_negative CHECK (quantity >= 0)
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
                                 PRIMARY KEY (id),
                                 CONSTRAINT uq_customer_email UNIQUE (email)
);

CREATE INDEX idx_customer_email ON customer(email);

ALTER TABLE public.customer_id_seq OWNER TO postgres;



CREATE TABLE public.buy_order (
                                  id integer DEFAULT nextval('public.buy_id_seq'::regclass) NOT NULL,
                                  customer_id integer NOT NULL,
                                  buy_order_uuid character varying(255),
                                  status character varying(255),
                                  PRIMARY KEY (id),
                                  FOREIGN KEY (customer_id) REFERENCES public.customer(id),
                                  CONSTRAINT uq_buy_order_uuid UNIQUE (buy_order_uuid)
);

CREATE INDEX idx_buy_order_uuid ON buy_order(buy_order_uuid);
CREATE INDEX idx_buy_order_customer_id ON buy_order(customer_id);
CREATE INDEX idx_buy_order_status ON buy_order(status);

ALTER TABLE public.buy_id_seq OWNER TO postgres;

CREATE TABLE public.order_details (
                                      buy_order_id integer NOT NULL,
                                      bread_id integer NOT NULL,
                                      quantity integer,
                                      price float,
                                      created_at timestamp without time zone,
                                      updated_at timestamp without time zone,
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

CREATE TABLE public.outbox (
                               id SERIAL PRIMARY KEY,
                               payload BYTEA NOT NULL,
                               sent BOOLEAN NOT NULL DEFAULT false,
                               created_at timestamp without time zone NOT NULL DEFAULT now()
);

ALTER TABLE public.outbox  OWNER TO postgres;


-- Admin users table
CREATE SEQUENCE public.admin_user_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.admin_users (
    id integer DEFAULT nextval('public.admin_user_id_seq'::regclass) NOT NULL,
    username character varying(255) UNIQUE NOT NULL,
    email character varying(255) UNIQUE NOT NULL,
    password character varying(255) NOT NULL,
    role character varying(50) DEFAULT 'admin',
    created_at timestamp without time zone DEFAULT now(),
    updated_at timestamp without time zone DEFAULT now(),
    PRIMARY KEY (id)
);

ALTER TABLE public.admin_user_id_seq OWNER TO postgres;

-- Invoices table
CREATE SEQUENCE public.invoice_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.invoices (
    id integer DEFAULT nextval('public.invoice_id_seq'::regclass) NOT NULL,
    buy_order_id integer NOT NULL,
    customer_id integer NOT NULL,
    invoice_number character varying(50) UNIQUE NOT NULL,
    subtotal float NOT NULL,
    tax float NOT NULL,
    total float NOT NULL,
    status character varying(50) DEFAULT 'pending',
    created_at timestamp without time zone DEFAULT now(),
    due_date timestamp without time zone,
    paid_at timestamp without time zone,
    PRIMARY KEY (id),
    FOREIGN KEY (buy_order_id) REFERENCES public.buy_order(id),
    FOREIGN KEY (customer_id) REFERENCES public.customer(id)
);

ALTER TABLE public.invoice_id_seq OWNER TO postgres;

-- Invoice items table
CREATE SEQUENCE public.invoice_item_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.invoice_items (
    id integer DEFAULT nextval('public.invoice_item_id_seq'::regclass) NOT NULL,
    invoice_id integer NOT NULL,
    bread_id integer NOT NULL,
    bread_name character varying(255),
    quantity integer NOT NULL,
    unit_price float NOT NULL,
    total float NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (invoice_id) REFERENCES public.invoices(id) ON DELETE CASCADE,
    FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);

ALTER TABLE public.invoice_item_id_seq OWNER TO postgres;

-- LISTEN/NOTIFY trigger: fires pg_notify('bakery_orders', buy_order_uuid)
-- whenever a buy_order row's status column changes value.
-- BuyBreadStream listens on this channel instead of polling the DB.
CREATE OR REPLACE FUNCTION public.notify_order_status_change()
RETURNS trigger AS $$
BEGIN
    IF NEW.status IS DISTINCT FROM OLD.status THEN
        PERFORM pg_notify('bakery_orders', NEW.buy_order_uuid);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER buy_order_status_notify
    AFTER UPDATE OF status ON public.buy_order
    FOR EACH ROW EXECUTE FUNCTION public.notify_order_status_change();