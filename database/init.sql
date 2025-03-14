CREATE SCHEMA IF NOT EXISTS tofl;

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS tofl.qa_pairs
(
    id              SERIAL PRIMARY KEY,
    question        TEXT NOT NULL,
    answer          TEXT NOT NULL,
    question_vector VECTOR(1536) -- Adjust dimension based on your model
    );