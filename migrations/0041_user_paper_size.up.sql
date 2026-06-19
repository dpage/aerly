-- A traveller's preferred paper size for the downloadable PDF itinerary
-- (Trip → Download PDF). Only two values are sensible: A4 (the global default)
-- and US Letter, so the formatter picks the page dimensions automatically from
-- this preference rather than asking each time. Existing rows default to 'a4'.
ALTER TABLE users
    ADD COLUMN paper_size TEXT NOT NULL DEFAULT 'a4'
        CHECK (paper_size IN ('a4', 'letter'));
