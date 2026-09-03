-- Reverses 0053. The kind text is descriptive only: nothing keys off it, so
-- dropping the column loses the description and no relationships.
ALTER TABLE hotel_details DROP COLUMN kind;
