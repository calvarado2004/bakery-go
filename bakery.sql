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

CREATE SEQUENCE public.pending_make_order_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- ========================================================================
-- Bread table
-- ========================================================================
CREATE TABLE public.bread (
    id integer DEFAULT nextval('public.bread_id_seq'::regclass) NOT NULL,
    name character varying(255),
    price numeric(10,2),
    quantity integer,
    description character varying(255),
    type character varying(255),
    status character varying(50),
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    image character varying(255),
    PRIMARY KEY (id),
    CONSTRAINT bread_quantity_non_negative CHECK (quantity >= 0),
    CONSTRAINT bread_status_valid CHECK (status IN ('available', 'unavailable', 'discontinued', 'out_of_stock', 'processing'))
);

ALTER TABLE public.bread_id_seq OWNER TO postgres;

-- ========================================================================
-- Bread indexes
-- ========================================================================
CREATE INDEX idx_bread_status ON bread(status);
CREATE INDEX idx_bread_quantity ON bread(quantity);
CREATE INDEX idx_bread_type ON bread(type);

-- ========================================================================
-- Bread Maker
-- ========================================================================
CREATE TABLE public.bread_maker (
    id integer DEFAULT nextval('public.bread_maker_id_seq'::regclass) NOT NULL,
    name character varying(255),
    email character varying(255),
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id)
);

ALTER TABLE public.bread_maker_id_seq OWNER TO postgres;

-- ========================================================================
-- Make Order
-- ========================================================================
CREATE TABLE public.make_order (
    id integer DEFAULT nextval('public.make_order_id_seq'::regclass) NOT NULL,
    bread_maker_id integer NOT NULL,
    make_order_uuid character varying(255),
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id),
    FOREIGN KEY (bread_maker_id) REFERENCES public.bread_maker(id)
);

CREATE INDEX idx_make_order_bread_maker_id ON make_order(bread_maker_id);
CREATE INDEX idx_make_order_uuid ON make_order(make_order_uuid);

CREATE TABLE public.make_order_details (
    make_order_id integer NOT NULL,
    bread_id integer NOT NULL,
    quantity integer,
    PRIMARY KEY (make_order_id, bread_id),
    FOREIGN KEY (make_order_id) REFERENCES public.make_order(id),
    FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);

-- ========================================================================
-- Pending Make Orders (server auto-replenishment requests)
-- External makers only consume from make-bread-order queue.
-- Auto-replenishment goes through this table instead.
-- ========================================================================
CREATE TABLE public.pending_make_orders (
    id integer DEFAULT nextval('public.pending_make_order_id_seq'::regclass) NOT NULL,
    bread_id integer NOT NULL,
    requested_quantity integer NOT NULL,
    status character varying(20) DEFAULT 'pending' CHECK (status IN ('pending', 'fulfilled', 'rejected')),
    source character varying(50) DEFAULT 'auto' CHECK (source IN ('auto', 'admin')),
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id),
    FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);

CREATE INDEX idx_pending_make_orders_status ON pending_make_orders(status) WHERE status = 'pending';

-- ========================================================================
-- Customer
-- ========================================================================
CREATE TABLE public.customer (
    id integer DEFAULT nextval('public.customer_id_seq'::regclass) NOT NULL,
    name character varying(255),
    email character varying(255),
    password character varying(255),
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT uq_customer_email UNIQUE (email)
);

CREATE INDEX idx_customer_email ON customer(email);

ALTER TABLE public.customer_id_seq OWNER TO postgres;

-- ========================================================================
-- Buy Order
-- ========================================================================
CREATE TABLE public.buy_order (
    id integer DEFAULT nextval('public.buy_id_seq'::regclass) NOT NULL,
    customer_id integer NOT NULL,
    buy_order_uuid character varying(255),
    status character varying(50),
    sequence_number bigint DEFAULT 0,
    bid_price numeric(10,2) DEFAULT 0,
    allow_partial boolean DEFAULT false,
    skip_unavailable_items boolean DEFAULT false,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id),
    FOREIGN KEY (customer_id) REFERENCES public.customer(id),
    CONSTRAINT uq_buy_order_uuid UNIQUE (buy_order_uuid),
    CONSTRAINT buy_order_status_valid CHECK (status IN ('pending', 'processing', 'processed', 'partially_processed', 'failed', 'rejected'))
);

CREATE INDEX idx_buy_order_uuid ON buy_order(buy_order_uuid);
CREATE INDEX idx_buy_order_customer_id ON buy_order(customer_id);
CREATE INDEX idx_buy_order_status ON buy_order(status);

-- ========================================================================
-- Order Details
-- ========================================================================
CREATE TABLE public.order_details (
    buy_order_id integer NOT NULL,
    bread_id integer NOT NULL,
    quantity integer,
    price numeric(10,2),
    status text DEFAULT 'pending',
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (buy_order_id, bread_id),
    FOREIGN KEY (buy_order_id) REFERENCES public.buy_order(id),
    FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);

CREATE INDEX idx_order_details_buy_order_id ON order_details(buy_order_id);

-- ========================================================================
-- Orders Processed
-- ========================================================================
CREATE TABLE public.orders_processed (
    id integer DEFAULT nextval('public.orders_processed_id_seq'::regclass) NOT NULL,
    customer_id integer NOT NULL,
    buy_order_id integer NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id),
    FOREIGN KEY (customer_id) REFERENCES public.customer(id),
    FOREIGN KEY (buy_order_id) REFERENCES public.buy_order(id)
);

CREATE INDEX idx_orders_processed_buy_order_id ON orders_processed(buy_order_id);

ALTER TABLE public.orders_processed_id_seq OWNER TO postgres;

-- ========================================================================
-- Outbox
-- ========================================================================
CREATE TABLE public.outbox (
    id SERIAL PRIMARY KEY,
    payload BYTEA NOT NULL,
    sent BOOLEAN NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE INDEX idx_outbox_sent ON outbox(sent) WHERE sent = false;
CREATE INDEX idx_outbox_created_at ON outbox(created_at);

ALTER TABLE public.outbox OWNER TO postgres;

-- ========================================================================
-- Admin Users
-- ========================================================================
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
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    PRIMARY KEY (id)
);

ALTER TABLE public.admin_user_id_seq OWNER TO postgres;

-- ========================================================================
-- Invoices
-- ========================================================================
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
    subtotal numeric(12,2) NOT NULL,
    tax numeric(12,2) NOT NULL,
    total numeric(12,2) NOT NULL,
    status character varying(50) DEFAULT 'pending',
    created_at timestamp with time zone DEFAULT now(),
    due_date timestamp with time zone,
    paid_at timestamp with time zone,
    PRIMARY KEY (id),
    FOREIGN KEY (buy_order_id) REFERENCES public.buy_order(id),
    FOREIGN KEY (customer_id) REFERENCES public.customer(id),
    CONSTRAINT invoices_status_valid CHECK (status IN ('pending', 'issued', 'paid', 'overdue', 'cancelled'))
);

ALTER TABLE public.invoice_id_seq OWNER TO postgres;

CREATE INDEX idx_invoices_buy_order_id ON invoices(buy_order_id);
CREATE INDEX idx_invoices_customer_id ON invoices(customer_id);
CREATE INDEX idx_invoices_status ON invoices(status);

-- ========================================================================
-- Invoice Items
-- ========================================================================
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
    unit_price numeric(10,2) NOT NULL,
    total numeric(10,2) NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (invoice_id) REFERENCES public.invoices(id) ON DELETE CASCADE,
    FOREIGN KEY (bread_id) REFERENCES public.bread(id)
);

ALTER TABLE public.invoice_item_id_seq OWNER TO postgres;

-- ========================================================================
-- LISTEN/NOTIFY trigger: fires pg_notify('bakery_orders', buy_order_uuid)
-- whenever a buy_order row's status column changes value.
-- BuyBreadStream listens on this channel instead of polling the DB.
-- ========================================================================
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
